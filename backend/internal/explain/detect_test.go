package explain

import (
	"errors"
	"testing"
)

func TestDetectFormat_Corpus(t *testing.T) {
	cases := []struct {
		file   string
		format Format
		code   string
	}{
		{file: "text_verbose_off.txt", format: FormatText},
		{file: "text_verbose_on.txt", format: FormatText},
		{file: "json_verbose_off.json", format: FormatJSON},
		{file: "json_verbose_on.json", format: FormatJSON},
		{file: "yaml.yaml", code: CodeUnsupportedFormat},
		{file: "xml.xml", code: CodeUnsupportedFormat},
	}

	for _, version := range versions {
		for _, c := range cases {
			t.Run(version+"/"+c.file, func(t *testing.T) {
				format, err := DetectFormat(fixture(t, version+"/"+c.file))

				if c.code != "" {
					var fe *Error
					if !errors.As(err, &fe) || fe.Code != c.code {
						t.Fatalf("want code %s, got format %q err %v", c.code, format, err)
					}

					return
				}

				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if format != c.format {
					t.Errorf("want %s, got %s", c.format, format)
				}
			})
		}
	}
}

func TestDetectFormat_Empty(t *testing.T) {
	if _, err := DetectFormat("  \n\t"); err == nil {
		t.Fatal("want an error on an empty body")
	}
}

func TestDetectFormat_ExplainArray(t *testing.T) {
	format, err := DetectFormat(fixture(t, "synthetic/generic_plan.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != FormatJSON {
		t.Errorf("want json, got %s", format)
	}
}
