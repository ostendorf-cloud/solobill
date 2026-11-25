package envsync

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
)

// images are the final stages of the Dockerfile that are published, each as
// solobill-<name>. checks.yaml and publish.yaml build exactly these, and the
// chart's two parts run them.
var images = []string{"api", "frontend"}

var (
	// dockerfileFrom is one FROM line with its optional stage name.
	dockerfileFrom = regexp.MustCompile(`(?m)^FROM (\S+)(?: AS (\S+))?$`)
	// workflowImages is the image list of a workflow matrix.
	workflowImages = regexp.MustCompile(`(?m)^\s+image: \[([^\]]*)\]$`)
)

// stage is one stage of the Dockerfile: its name and its lines after FROM.
type stage struct {
	name string
	from string
	body string
}

// readStages splits the Dockerfile into its stages, in order.
func readStages(t *testing.T) []stage {
	t.Helper()

	docker, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(docker)
	locs := dockerfileFrom.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		t.Fatal("Dockerfile has no FROM line")
	}

	stages := make([]stage, 0, len(locs))
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		s := stage{from: text[loc[2]:loc[3]], body: text[loc[1]:end]}
		if loc[4] >= 0 {
			s.name = text[loc[4]:loc[5]]
		}
		stages = append(stages, s)
	}

	return stages
}

func findStage(t *testing.T, stages []stage, name string) stage {
	t.Helper()

	for _, s := range stages {
		if s.name == name {
			return s
		}
	}
	t.Fatalf("Dockerfile has no stage %q", name)

	return stage{}
}

// TestDockerfileBuildsTwoImages pins the split of issue 149: the API image
// carries the API binary and typst and nothing of the Vue build, the
// frontend image carries the Vue build and its server and no typst, and a
// build with no --target builds the frontend, the last stage.
func TestDockerfileBuildsTwoImages(t *testing.T) {
	t.Parallel()

	stages := readStages(t)
	if last := stages[len(stages)-1].name; last != "frontend" {
		t.Errorf("the last Dockerfile stage is %q, want frontend", last)
	}

	api := findStage(t, stages, "api")
	for _, want := range []string{
		"COPY --from=build-api /out/solobill /usr/local/bin/solobill",
		"COPY --from=typst /out/typst /usr/local/bin/typst",
		`ENTRYPOINT ["/usr/local/bin/solobill"]`,
		"USER 65532:65532",
	} {
		if !strings.Contains(api.body, want) {
			t.Errorf("the api stage lacks %q", want)
		}
	}
	if strings.Contains(api.body, "--from=web") || strings.Contains(api.body, "--from=build-frontend") {
		t.Error("the api stage copies from the web or frontend build, it must carry the API only")
	}

	frontend := findStage(t, stages, "frontend")
	for _, want := range []string{
		"COPY --from=build-frontend /out/frontend /usr/local/bin/frontend",
		"COPY --from=web /src/web/dist/ " + frontendconfig.DefaultBuildDir + "/",
		`ENTRYPOINT ["/usr/local/bin/frontend"]`,
		"USER 65532:65532",
	} {
		if !strings.Contains(frontend.body, want) {
			t.Errorf("the frontend stage lacks %q", want)
		}
	}
	if strings.Contains(frontend.body, "--from=typst") || strings.Contains(frontend.body, "--from=build-api") {
		t.Error("the frontend stage copies typst or the API binary, it must carry the frontend only")
	}

	// The Go build of the API must not need the Node stage at all, so
	// "--target api" never runs it.
	if build := findStage(t, stages, "build-api"); strings.Contains(build.body, "--from=web") {
		t.Error("the build-api stage copies from the web stage")
	}

	// Both runtime images use the same pinned base.
	if api.from != frontend.from {
		t.Errorf("api runs on %s but frontend on %s, want the same pinned base", api.from, frontend.from)
	}
	if !strings.Contains(api.from, "@sha256:") {
		t.Errorf("the runtime base %s is not pinned by digest", api.from)
	}
}

// TestWorkflowsBuildEveryImage fails when checks.yaml or publish.yaml build
// another set of images than the Dockerfile's published stages.
func TestWorkflowsBuildEveryImage(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"../../.github/workflows/checks.yaml", "../../.github/workflows/publish.yaml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		matches := workflowImages.FindAllStringSubmatch(string(data), -1)
		if len(matches) == 0 {
			t.Errorf("%s has no 'image: [...]' matrix", path)
		}
		for _, m := range matches {
			var got []string
			for item := range strings.SplitSeq(m[1], ",") {
				got = append(got, strings.TrimSpace(item))
			}
			slices.Sort(got)
			if !slices.Equal(got, images) {
				t.Errorf("%s builds the images %q, want %q", path, got, images)
			}
		}
	}
}
