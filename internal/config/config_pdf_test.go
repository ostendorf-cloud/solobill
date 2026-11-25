package config

import "testing"

func TestLoadPDF(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  PDFConfig
	}{
		{name: "unset leaves issuing off", input: "", want: PDFConfig{}},
		{name: "only whitespace is unset", input: "  \t", want: PDFConfig{}},
		{name: "a name on PATH", input: "typst", want: PDFConfig{Enabled: true, TypstBinary: "typst"}},
		{name: "an absolute path", input: "/usr/local/bin/typst", want: PDFConfig{Enabled: true, TypstBinary: "/usr/local/bin/typst"}},
		{name: "surrounding whitespace is trimmed", input: " typst\n", want: PDFConfig{Enabled: true, TypstBinary: "typst"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(validEnv(t, map[string]string{"PDF_TYPST_BINARY": tt.input}))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.PDF != tt.want {
				t.Errorf("PDF = %+v, want %+v", cfg.PDF, tt.want)
			}
		})
	}
}
