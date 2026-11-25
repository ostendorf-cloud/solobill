package envsync

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	// chartTypst is the typst path the chart hands the API as PDF_TYPST_BINARY.
	chartTypst = regexp.MustCompile(`(?m)^\s+typstBinary: (\S+)$`)
	// chartImageRepo is an image repository default of the chart.
	chartImageRepo = regexp.MustCompile(`(?m)^\s+repository: (ghcr\.io/ostendorf-cloud/solobill\S*)$`)
)

// TestChartTypstMatchesImage fails when the chart hands the API another
// typst path than the one the api stage of the Dockerfile installs.
func TestChartTypstMatchesImage(t *testing.T) {
	t.Parallel()

	values, err := os.ReadFile("../../charts/solobill/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := chartTypst.FindAllStringSubmatch(string(values), -1)
	if len(m) != 1 {
		t.Fatalf("charts/solobill/values.yaml must have exactly one 'typstBinary: <path>' line, got %q", m)
	}
	api := findStage(t, readStages(t), "api")
	if !strings.Contains(api.body, "COPY --from=typst /out/typst "+m[0][1]+"\n") {
		t.Errorf("the chart sets PDF_TYPST_BINARY to %s, but the api stage installs typst elsewhere", m[0][1])
	}
}

// TestChartImagesMatchPublish fails when the chart's image repositories are
// not the ones publish.yaml pushes, ghcr.io/<owner>/solobill-<image>.
func TestChartImagesMatchPublish(t *testing.T) {
	t.Parallel()

	values, err := os.ReadFile("../../charts/solobill/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range chartImageRepo.FindAllStringSubmatch(string(values), -1) {
		got = append(got, m[1])
	}
	want := []string{"ghcr.io/ostendorf-cloud/solobill-api", "ghcr.io/ostendorf-cloud/solobill-frontend"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("charts/solobill/values.yaml image repositories = %q, want %q in that order", got, want)
	}

	publish, err := os.ReadFile("../../.github/workflows/publish.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(publish), "IMAGE: ghcr.io/${{ github.repository_owner }}/solobill-${{ matrix.image }}") {
		t.Error("publish.yaml does not push ghcr.io/<owner>/solobill-<image>")
	}
}
