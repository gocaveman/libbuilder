package libbuilder

import (
	"io/ioutil"
	"testing"
)

func TestYarnBuild(t *testing.T) {

	tmpDir, err := ioutil.TempDir("", "TestYarnBuild")
	if err != nil {
		t.Fatal(err)
	}
	// defer os.RemoveAll(tmpDir)
	builder := &Builder{
		DebugKeepTemp: true,
	}

	opts := &YarnBuildOptions{
		SrcName:     "jquery",
		SrcFilePath: "dist/jquery.js",
		OutBaseDir:  tmpDir,
		GoName:      "jquery",
		Type:        "js",
	}

	// build latest, twice
	opts.Version = "latest"
	err = builder.YarnBuild(opts)
	if err != nil {
		t.Fatal(err)
	}

	opts.Version = "latest"
	err = builder.YarnBuild(opts)
	if err != nil {
		t.Fatal(err)
	}

	// check another version
	opts.Version = "2.1.0"
	err = builder.YarnBuild(opts)
	if err != nil {
		t.Fatal(err)
	}

}
