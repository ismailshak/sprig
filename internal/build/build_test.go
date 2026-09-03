package build

import "testing"

func TestRead_PopulatesGoVersion(t *testing.T) {
	info := Read()
	if info.GoVersion == "" {
		t.Error("GoVersion was empty; test binaries carry build info since Go 1.18")
	}
}
