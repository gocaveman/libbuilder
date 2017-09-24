package libbuilder

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
)

var ErrNotFound = fmt.Errorf("not found")

type Builder struct {
	// OutBaseDir string
	DebugKeepTemp bool
}

type YarnBuildOptions struct {
	SrcName     string   `json:"src_name"`      // yarn package name, e.g. "jquery"
	SrcFilePath string   `json:"src_file_path"` // source file in yarn dir after adding, e.g. "dist/jquery.js" (the "node_modules/jquery" prefix is implied)
	OutBaseDir  string   `json:"out_base_dir"`  // absolute path prefix to which only the version is appended, e.g. "/path/to/libs/jquery" (to which "latest" or "3.1.0", etc. is added)
	GoName      string   `json:"go_name"`       // the name of the package in Go, used to generate the "package ..." statement and to generate the path
	Type        string   `json:"type"`          // "js" or "css"
	JSDeps      []string `json:"js_deps"`       // list of UIRegistry JS dependencies in semver format (where the package name is the GoName of the other package), e.g. "bootstrap"
	CSSDeps     []string `json:"css_deps"`      // list of UIRegistry CSS dependencies in semver format (where the package name is the GoName of the other package), e.g. "bootstrap"
	// FIXME: is there a concept of "conflicts with?" (RPM has this...)
	GoImports []string `json:"go_imports` // list of go imports that should be included in the generated output

	Version string `json:"version"` // either a version number or "latest"
}

func (b *Builder) YarnBuild(opts *YarnBuildOptions) error {

	var allVers []string
	var latestVer, betaVer string
	var err error

	if opts.Version == "" {
		return fmt.Errorf("version must be specified in options")
	}

	if /*opts.Version == "" ||*/ opts.Version == "latest" {

		log.Printf("Fetching yarn versions for %q", opts.SrcName)
		allVers, latestVer, betaVer, err = b.yarnFetchVersions(opts.SrcName)
		if err != nil {
			return err
		}
		log.Printf("Found versions: %#v (latest=%q, beta=%q)", allVers, latestVer, betaVer)

		// if opts.Version == "latest" {
		// if latestVer != "" {
		log.Printf("Building latest version (%q) for %q", latestVer, opts.SrcName)
		err = b.yarnBuildOne(latestVer, opts)
		if err != nil {
			return err
		}
		// }
		return nil
		// }

		// if betaVer != "" {
		// 	log.Printf("Building beta for %q", opts.SrcName)
		// 	err = b.yarnBuildOne(betaVer, opts)
		// 	if err != nil {
		// 		return err
		// 	}
		// }

		// log.Printf("Building remaining versions for %q", opts.SrcName)
		// for _, ver := range allVers {
		// 	err = b.yarnBuildOne(ver, opts)
		// 	if err != nil {
		// 		return err
		// 	}
		// }

		// return nil
	}

	return b.yarnBuildOne(opts.Version, opts)
}

func (b *Builder) yarnBuildOne(ver string, opts *YarnBuildOptions) error {

	var err error

	log.Printf("yarnBuildOne: Building version %q for %q", ver, opts.SrcName)

	origWd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(origWd) // restore working directory

	// make empty bare git repo at correct location if not exists
	targetDir := filepath.Join(opts.OutBaseDir, ver)
	log.Printf("targetDir: %q", targetDir)
	os.MkdirAll(targetDir, 0775)
	_, err = combinedOutput("git", "init", "--bare", targetDir)
	if err != nil {
		return err
	}

	// pull down yarn stuff
	yarnTmpDir, err := ioutil.TempDir("", "yarnBuildOne-yarn-add-")
	if err != nil {
		return err
	}
	if !b.DebugKeepTemp {
		defer os.RemoveAll(yarnTmpDir)
	}

	err = ioutil.WriteFile(filepath.Join(yarnTmpDir, "package.json"), []byte(`{}`), 0644)
	if err != nil {
		return err
	}

	os.Chdir(yarnTmpDir)
	_, err = combinedOutput("yarn", "add", "-E", opts.SrcName+"@"+ver)
	if err != nil {
		return err
	}

	// make temp dir for git checkout

	gitTmpDir, err := ioutil.TempDir("", "yarnBuildOne-git-checkout-")
	if err != nil {
		return err
	}
	if !b.DebugKeepTemp {
		defer os.RemoveAll(gitTmpDir)
	}

	os.Chdir(gitTmpDir)
	_, err = combinedOutput("git", "clone", targetDir, gitTmpDir)
	if err != nil {
		return err
	}

	fname := path.Base(opts.SrcFilePath)

	// copy target file
	fromPath := filepath.Join(yarnTmpDir, "node_modules", opts.SrcName, opts.SrcFilePath)
	// toPath := filepath.Join(targetDir, fname)
	toPath := filepath.Join(gitTmpDir, fname)
	log.Printf("Copying file %q to %q", fromPath, toPath)
	err = CopyFile(fromPath, toPath)
	if err != nil {
		return err
	}

	toPathF, err := os.Open(toPath)
	if err != nil {
		return err
	}
	defer toPathF.Close()

	// figure out target file for Go code generation
	goFilePath := filepath.Join(gitTmpDir, "lib.go")
	err = generateLibGo(goFilePath, toPathF, ver, opts)
	if err != nil {
		return err
	}

	// cd to correct dir for git stuff
	os.Chdir(gitTmpDir)

	// git commit & push
	err = combinedOutputDump("git", "add", fname, filepath.Base(goFilePath))
	if err != nil {
		return err
	}

	statusb, err := combinedOutput("git", "status", "--porcelain")
	if err != nil {
		return err
	}

	// if this resulted in any change, then commit and push
	if len(bytes.TrimSpace(statusb)) > 0 {

		err = combinedOutputDump("git", "commit", "-m", "latest and greatest")
		if err != nil {
			return err
		}

		err = combinedOutputDump("git", "push")
		if err != nil {
			return err
		}

	}

	return nil

}

func (b *Builder) yarnFetchVersions(yarnPackageName string) (retVers []string, latestVer string, betaVer string, retErr error) {

	tmpDir, err := ioutil.TempDir("", "yarnFetchVersions")
	if err != nil {
		return nil, "", "", err
	}
	if !b.DebugKeepTemp {
		defer os.RemoveAll(tmpDir)
	}

	err = ioutil.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{}`), 0644)
	if err != nil {
		return nil, "", "", err
	}

	bts, err := combinedOutput("yarn", "info", "--json", yarnPackageName)
	if err != nil {
		return nil, "", "", err
	}

	r := bufio.NewReader(bytes.NewBuffer(bts))
	for {
		lineb, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", "", err
		}

		info := struct {
			Type string `json:"type"`
			Data struct {
				DistTags struct {
					Latest string `json:"latest"`
					Beta   string `json:"beta"`
				} `json:"dist-tags"`
				Versions []string `json:"versions"`
			} `json:"data"`
		}{}

		err = json.Unmarshal(lineb, &info)
		if err != nil {
			return nil, "", "", err
		}

		if info.Type != "inspect" {
			continue
		}

		return info.Data.Versions, info.Data.DistTags.Latest, info.Data.DistTags.Beta, nil

	}

	return nil, "", "", ErrNotFound

}

// generateLibGo code generates the Go file to go into the library folder
func generateLibGo(goFilePath string, inFile io.Reader, ver string, opts *YarnBuildOptions) error {

	inFileB, err := ioutil.ReadAll(inFile)
	if err != nil {
		return err
	}

	s := fmt.Sprintf(`
package %s_%s

import "github.com/bradleypeabody/gocaveman/uiregistry"
// TODO: go imports

func init() {

	ds := uiregistry.NewBytesDataSource([]byte(%q))

	e := uiregistry.Entry {
		Type: %q,
		Name: %q
		Version: %q,
		FileName: %q,
		Deps: %#v,
		DataSource: ds,
	}

	err := uiregistry.Global.Register(e)
	if err != nil {
		panic(err)
	}

}
`,
		opts.GoName, regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllLiteralString(ver, "_"), // package name
		string(inFileB), // file contents
		opts.Type,       // type
		opts.GoName,     // name
		ver,             // version
		// opts.SrcName + "-" + ver + "." + opts.Type, // filename
		path.Base(opts.SrcFilePath), // filename
		[]string{},                  // FIXME: deps
	)

	err = ioutil.WriteFile(goFilePath, []byte(s), 0644)
	if err != nil {
		return err
	}

	return nil
}

func combinedOutputDump(cmd string, args ...string) error {

	b, err := combinedOutput(cmd, args...)
	log.Printf("RESULT:\n%s", b)

	return err

}

func combinedOutput(cmd string, args ...string) ([]byte, error) {

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%q ", cmd)
	for _, arg := range args {
		fmt.Fprintf(&buf, "%q ", arg)
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	log.Printf("RUNNING (dir=%q): %s", wd, buf.String())

	return exec.Command(cmd, args...).CombinedOutput()

}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
// See https://stackoverflow.com/questions/21060945/simple-way-to-copy-a-file-in-golang
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}
