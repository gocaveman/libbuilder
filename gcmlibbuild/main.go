package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/gocaveman/libbuilder"
)

var buildAllTo = flag.String("build-all-to", "", "Build all of the default libraries to the specified output directory, e.g. /path/to/libs")
var yarn = flag.String("yarn", "", "Build using the specified YarnBuildOptions, parsed from this JSON")

func main() {

	flag.Parse()

	builder := &libbuilder.Builder{}

	if *yarn != "" {
		var yarnBuildOptions libbuilder.YarnBuildOptions
		err := json.Unmarshal([]byte(*yarn), &yarnBuildOptions)
		if err != nil {
			log.Fatal(err)

		}
		err = builder.YarnBuild(&yarnBuildOptions)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	if len(*buildAllTo) > 0 {
		doBuildAll(builder, *buildAllTo)
		return
	}

	log.Fatal("no action specified, check -help")
	return
}

func doBuildAll(builder *libbuilder.Builder, outDir string) {

	if err := builder.YarnBuild(&libbuilder.YarnBuildOptions{
		SrcName:     "jquery",
		SrcFilePath: "dist/jquery.js",
		OutBaseDir:  mkdir(filepath.Join(outDir, "jquery")),
		GoName:      "jquery",
		Type:        "js",
		Version:     "latest",
	}); err != nil {
		log.Fatal(err)
	}

	// TODO:
	// - other versions of jquery
	// - vue
	// - bootstrap
	// - underscore

}

func mkdir(p string) string {
	os.Mkdir(p, 0775)
	return p
}
