// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dep

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"github.com/golang/dep/internal/gps"
	"github.com/golang/dep/internal/test"
)

var (
	discardLogger = log.New(ioutil.Discard, "", 0)
)

func TestSplitAbsoluteProjectRoot(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")

	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := []string{
		"github.com/pkg/errors",
		"my/silly/thing",
	}

	for _, want := range importPaths {
		fullpath := filepath.Join(depCtx.GOPATH, "src", want)
		got, err := depCtx.SplitAbsoluteProjectRoot(fullpath)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	}

	// test where it should return an error when directly within $GOPATH/src
	got, err := depCtx.SplitAbsoluteProjectRoot(filepath.Join(depCtx.GOPATH, "src"))
	if err == nil || !strings.Contains(err.Error(), "GOPATH/src") {
		t.Fatalf("should have gotten an error for use directly in GOPATH/src, but got %s", got)
	}

	// test where it should return an error
	got, err = depCtx.SplitAbsoluteProjectRoot("tra/la/la/la")
	if err == nil {
		t.Fatalf("should have gotten an error but did not for tra/la/la/la: %s", got)
	}
}

func TestAbsoluteProjectRoot(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := map[string]bool{
		"github.com/pkg/errors": true,
		"my/silly/thing":        false,
	}

	for i, create := range importPaths {
		if create {
			h.TempDir(filepath.Join("src", i))
		}
	}

	for i, ok := range importPaths {
		got, err := depCtx.absoluteProjectRoot(i)
		if ok {
			h.Must(err)
			want := h.Path(filepath.Join("src", i))
			if got != want {
				t.Fatalf("expected %s, got %q", want, got)
			}
			continue
		}

		if err == nil {
			t.Fatalf("expected %s to fail", i)
		}
	}

	// test that a file fails
	h.TempFile("src/thing/thing.go", "hello world")
	_, err := depCtx.absoluteProjectRoot("thing/thing.go")
	if err == nil {
		t.Fatal("error should not be nil for a file found")
	}
}

func TestVersionInWorkspace(t *testing.T) {
	test.NeedsExternalNetwork(t)
	test.NeedsGit(t)

	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := map[string]struct {
		rev      gps.Version
		checkout bool
	}{
		"github.com/pkg/errors": {
			rev:      gps.NewVersion("v0.8.0").Pair("645ef00459ed84a119197bfb8d8205042c6df63d"), // semver
			checkout: true,
		},
		"github.com/Sirupsen/logrus": {
			rev:      gps.Revision("42b84f9ec624953ecbf81a94feccb3f5935c5edf"), // random sha
			checkout: true,
		},
		"github.com/rsc/go-get-default-branch": {
			rev: gps.NewBranch("another-branch").Pair("8e6902fdd0361e8fa30226b350e62973e3625ed5"),
		},
	}

	// checkout the specified revisions
	for ip, info := range importPaths {
		h.RunGo("get", ip)
		repoDir := h.Path("src/" + ip)
		if info.checkout {
			h.RunGit(repoDir, "checkout", info.rev.String())
		}

		got, err := depCtx.VersionInWorkspace(gps.ProjectRoot(ip))
		h.Must(err)

		if got != info.rev {
			t.Fatalf("expected %q, got %q", got.String(), info.rev.String())
		}
	}
}

func TestLoadProject(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir(filepath.Join("src", "test1", "sub"))
	h.TempFile(filepath.Join("src", "test1", ManifestName), "")
	h.TempFile(filepath.Join("src", "test1", LockName), `memo = "cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee"`)
	h.TempDir(filepath.Join("src", "test2", "sub"))
	h.TempFile(filepath.Join("src", "test2", ManifestName), "")

	var testcases = []struct {
		name string
		lock bool
		wd   string
	}{
		{"direct", true, filepath.Join("src", "test1")},
		{"ascending", true, filepath.Join("src", "test1", "sub")},
		{"without lock", false, filepath.Join("src", "test2")},
		{"ascending without lock", false, filepath.Join("src", "test2", "sub")},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Ctx{
				Out: discardLogger,
				Err: discardLogger,
			}

			err := ctx.SetPaths(h.Path(tc.wd), h.Path("."))
			if err != nil {
				t.Fatalf("%+v", err)
			}

			p, err := ctx.LoadProject()
			switch {
			case err != nil:
				t.Fatalf("%s: LoadProject failed: %+v", tc.wd, err)
			case p.Manifest == nil:
				t.Fatalf("%s: Manifest file didn't load", tc.wd)
			case tc.lock && p.Lock == nil:
				t.Fatalf("%s: Lock file didn't load", tc.wd)
			case !tc.lock && p.Lock != nil:
				t.Fatalf("%s: Non-existent Lock file loaded", tc.wd)
			}
		})
	}
}

func TestLoadProjectNotFoundErrors(t *testing.T) {
	tg := test.NewHelper(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempDir("src/test1/sub")
	tg.Setenv("GOPATH", tg.Path("."))

	var testcases = []struct {
		lock  bool
		start string
		path  string
	}{
		{true, filepath.Join("src", "test1"), ""},        //direct
		{true, filepath.Join("src", "test1", "sub"), ""}, //ascending
	}

	for _, testcase := range testcases {
		ctx := &Ctx{GOPATHs: []string{tg.Path(".")}, WorkingDir: tg.Path(testcase.start)}

		_, err := ctx.LoadProject()
		if err == nil {
			t.Errorf("%s: should have returned 'No Manifest Found' error", testcase.start)
		}
	}
}

func TestLoadProjectManifestParseError(t *testing.T) {
	tg := test.NewHelper(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempFile(filepath.Join("src/test1", ManifestName), `[[constraint]]`)
	tg.TempFile(filepath.Join("src/test1", LockName), `memo = "cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee"\n\n[[projects]]`)
	tg.Setenv("GOPATH", tg.Path("."))

	path := filepath.Join("src", "test1")
	tg.Cd(tg.Path(path))

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal("failed to get working directory", err)
	}

	ctx := &Ctx{
		GOPATH:     tg.Path("."),
		WorkingDir: wd,
		Out:        discardLogger,
		Err:        discardLogger,
	}

	_, err = ctx.LoadProject()
	if err == nil {
		t.Fatal("should have returned 'Manifest Syntax' error")
	}
}

func TestLoadProjectLockParseError(t *testing.T) {
	tg := test.NewHelper(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempFile(filepath.Join("src/test1", ManifestName), `[[constraint]]`)
	tg.TempFile(filepath.Join("src/test1", LockName), `memo = "cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee"\n\n[[projects]]`)
	tg.Setenv("GOPATH", tg.Path("."))

	path := filepath.Join("src", "test1")
	tg.Cd(tg.Path(path))

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal("failed to get working directory", err)
	}

	ctx := &Ctx{
		GOPATH:     tg.Path("."),
		WorkingDir: wd,
		Out:        discardLogger,
		Err:        discardLogger,
	}

	_, err = ctx.LoadProject()
	if err == nil {
		t.Fatal("should have returned 'Lock Syntax' error")
	}
}

func TestLoadProjectNoSrcDir(t *testing.T) {
	tg := test.NewHelper(t)
	defer tg.Cleanup()

	tg.TempDir("test1")
	tg.TempFile(filepath.Join("test1", ManifestName), `[[constraint]]`)
	tg.TempFile(filepath.Join("test1", LockName), `memo = "cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee"\n\n[[projects]]`)
	tg.Setenv("GOPATH", tg.Path("."))

	ctx := &Ctx{GOPATH: tg.Path(".")}
	path := filepath.Join("test1")
	tg.Cd(tg.Path(path))

	f, _ := os.OpenFile(filepath.Join(ctx.GOPATH, "src", "test1", LockName), os.O_WRONLY, os.ModePerm)
	defer f.Close()

	_, err := ctx.LoadProject()
	if err == nil {
		t.Fatal("should have returned 'Split Absolute Root' error (no 'src' dir present)")
	}
}

// TestCaseInsentitive is test for Windows. This should work even though set
// difference letter cases in GOPATH.
func TestCaseInsentitiveGOPATH(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skip this test on non-Windows")
	}

	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.TempDir("src/test1")
	h.TempFile(filepath.Join("src/test1", ManifestName), `[[constraint]]`)

	// Shuffle letter case
	rs := []rune(strings.ToLower(h.Path(".")))
	for i, r := range rs {
		if unicode.IsLower(r) {
			rs[i] = unicode.ToUpper(r)
		} else {
			rs[i] = unicode.ToLower(r)
		}
	}
	gopath := string(rs)
	h.Setenv("GOPATH", gopath)
	wd := h.Path("src/test1")

	depCtx := &Ctx{}
	if err := depCtx.SetPaths(wd, gopath); err != nil {
		t.Fatal(err)
	}
	if _, err := depCtx.LoadProject(); err != nil {
		t.Fatal(err)
	}

	ip := "github.com/pkg/errors"
	fullpath := filepath.Join(depCtx.GOPATH, "src", ip)
	pr, err := depCtx.SplitAbsoluteProjectRoot(fullpath)
	if err != nil {
		t.Fatal(err)
	}
	if pr != ip {
		t.Fatalf("expected %s, got %s", ip, pr)
	}
}

func TestDetectProjectGOPATH(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("go")
	h.TempDir("go-two")

	ctx := &Ctx{
		GOPATHs: []string{h.Path("go"), h.Path("go-two")},
	}

	h.TempDir("go/src/real/path")
	h.TempDir("go/src/sym")

	// Another directory used as a GOPATH
	h.TempDir("go-two/src/real/path")
	h.TempDir("go-two/src/sym")

	h.TempDir("sym") // Directory for symlinks

	testcases := []struct {
		name         string
		root         string
		resolvedRoot string
		GOPATH       string
		expectErr    bool
	}{
		{
			name:         "project-with-no-AbsRoot",
			root:         "",
			resolvedRoot: filepath.Join(ctx.GOPATHs[0], "src", "real", "path"),
			expectErr:    true,
		},
		{
			name:         "project-with-no-ResolvedAbsRoot",
			root:         filepath.Join(ctx.GOPATHs[0], "src", "real", "path"),
			resolvedRoot: "",
			expectErr:    true,
		},
		{
			name:         "AbsRoot-is-not-within-any-GOPATH",
			root:         filepath.Join(h.Path("."), "src", "real", "path"),
			resolvedRoot: filepath.Join(h.Path("."), "src", "real", "path"),
			expectErr:    true,
		},
		{
			name:         "neither-AbsRoot-nor-ResolvedAbsRoot-are-in-any-GOPATH",
			root:         filepath.Join(h.Path("."), "src", "sym", "path"),
			resolvedRoot: filepath.Join(h.Path("."), "src", "real", "path"),
			expectErr:    true,
		},
		{
			name:         "both-AbsRoot-and-ResolvedAbsRoot-are-in-the-same-GOPATH",
			root:         filepath.Join(ctx.GOPATHs[0], "src", "sym", "path"),
			resolvedRoot: filepath.Join(ctx.GOPATHs[0], "src", "real", "path"),
			expectErr:    true,
		},
		{
			name:         "AbsRoot-and-ResolvedAbsRoot-are-each-within-a-different-GOPATH",
			root:         filepath.Join(ctx.GOPATHs[0], "src", "sym", "path"),
			resolvedRoot: filepath.Join(ctx.GOPATHs[1], "src", "real", "path"),
			expectErr:    true,
		},
		{
			name:         "AbsRoot-is-not-a-symlink",
			root:         filepath.Join(ctx.GOPATHs[0], "src", "real", "path"),
			resolvedRoot: filepath.Join(ctx.GOPATHs[0], "src", "real", "path"),
			GOPATH:       ctx.GOPATHs[0],
		},
		{
			name:         "AbsRoot-is-a-symlink-to-ResolvedAbsRoot",
			root:         filepath.Join(h.Path("."), "sym", "symlink"),
			resolvedRoot: filepath.Join(ctx.GOPATHs[0], "src", " real", "path"),
			GOPATH:       ctx.GOPATHs[0],
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			project := &Project{
				AbsRoot:         tc.root,
				ResolvedAbsRoot: tc.resolvedRoot,
			}

			GOPATH, err := ctx.DetectProjectGOPATH(project)
			if !tc.expectErr && err != nil {
				t.Fatalf("%+v", err)
			} else if tc.expectErr && err == nil {
				t.Fatalf("expected an error, got nil and gopath %s", GOPATH)
			}
			if GOPATH != tc.GOPATH {
				t.Errorf("expected GOPATH %s, got %s", tc.GOPATH, GOPATH)
			}
		})
	}
}

func TestDetectGOPATH(t *testing.T) {
	th := test.NewHelper(t)
	defer th.Cleanup()

	th.TempDir("go")
	th.TempDir("gotwo")

	ctx := &Ctx{GOPATHs: []string{
		th.Path("go"),
		th.Path("gotwo"),
	}}

	testcases := []struct {
		GOPATH string
		path   string
		err    bool
	}{
		{th.Path("go"), filepath.Join(th.Path("go"), "src/github.com/username/package"), false},
		{th.Path("go"), filepath.Join(th.Path("go"), "src/github.com/username/package"), false},
		{th.Path("gotwo"), filepath.Join(th.Path("gotwo"), "src/github.com/username/package"), false},
		{"", filepath.Join(th.Path("."), "code/src/github.com/username/package"), true},
	}

	for _, tc := range testcases {
		GOPATH, err := ctx.detectGOPATH(tc.path)
		if tc.err && err == nil {
			t.Error("Expected error but got none")
		}
		if GOPATH != tc.GOPATH {
			t.Errorf("Expected GOPATH to be %s, got %s", GOPATH, tc.GOPATH)
		}
	}
}
