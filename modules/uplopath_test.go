package modules

import (
	"runtime"
	"testing"

	"github.com/uplo-tech/errors"
)

var (
	// TestGlobalUploPathVar tests that the NewGlobalUploPath initialization
	// works.
	TestGlobalUploPathVar UploPath = NewGlobalUploPath("/testdir")
)

// TestGlobalUploPath checks that initializing a new global uplopath does not
// cause any issues.
func TestGlobalUploPath(t *testing.T) {
	sp, err := TestGlobalUploPathVar.Join("testfile")
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := NewUploPath("/testdir")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := mirror.Join("testfile")
	if err != nil {
		t.Fatal(err)
	}
	if !sp.Equals(expected) {
		t.Error("the separately spawned uplopath should equal the global uplopath")
	}
}

// TestRandomUploPath tests that RandomUploPath always returns a valid UploPath
func TestRandomUploPath(t *testing.T) {
	for i := 0; i < 1000; i++ {
		err := RandomUploPath().Validate(false)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestUplopathValidate verifies that the validate function correctly validates
// UploPaths.
func TestUplopathValidate(t *testing.T) {
	var pathtests = []struct {
		in    string
		valid bool
	}{
		{"valid/uplopath", true},
		{"../../../directory/traversal", false},
		{"testpath", true},
		{"valid/uplopath/../with/directory/traversal", false},
		{"validpath/test", true},
		{"..validpath/..test", true},
		{"./invalid/path", false},
		{".../path", true},
		{"valid./path", true},
		{"valid../path", true},
		{"valid/path./test", true},
		{"valid/path../test", true},
		{"test/path", true},
		{"/leading/slash", false},
		{"foo/./bar", false},
		{"", false},
		{"blank/end/", false},
		{"double//dash", false},
		{"../", false},
		{"./", false},
		{".", false},
	}
	for _, pathtest := range pathtests {
		err := ValidatePathString(pathtest.in, false)
		if err != nil && pathtest.valid {
			t.Fatal("validateUplopath failed on valid path: ", pathtest.in)
		}
		if err == nil && !pathtest.valid {
			t.Fatal("validateUplopath succeeded on invalid path: ", pathtest.in)
		}
	}
}

// TestUplopath tests that the NewUploPath, LoadString, and Join methods function correctly
func TestUplopath(t *testing.T) {
	var pathtests = []struct {
		in    string
		valid bool
		out   string
	}{
		{`\\some\\windows\\path`, true, `\\some\\windows\\path`}, // if the os is not windows this will not update the separators
		{"valid/uplopath", true, "valid/uplopath"},
		{`\some\back\slashes\path`, true, `\some\back\slashes\path`},
		{"../../../directory/traversal", false, ""},
		{"testpath", true, "testpath"},
		{"valid/uplopath/../with/directory/traversal", false, ""},
		{"validpath/test", true, "validpath/test"},
		{"..validpath/..test", true, "..validpath/..test"},
		{"./invalid/path", false, ""},
		{".../path", true, ".../path"},
		{"valid./path", true, "valid./path"},
		{"valid../path", true, "valid../path"},
		{"valid/path./test", true, "valid/path./test"},
		{"valid/path../test", true, "valid/path../test"},
		{"test/path", true, "test/path"},
		{"/leading/slash", true, "leading/slash"}, // clean will trim leading slashes so this is a valid input
		{"foo/./bar", false, ""},
		{"", false, ""},
		{`\`, true, `\`},
		{`\\`, true, `\\`},
		{`\\\`, true, `\\\`},
		{`\\\\`, true, `\\\\`},
		{`\\\\\`, true, `\\\\\`},
		{"/", false, ""},
		{"//", false, ""},
		{"///", false, ""},
		{"////", false, ""},
		{"/////", false, ""},
		{"blank/end/", true, "blank/end"}, // clean will trim trailing slashes so this is a valid input
		{"double//dash", false, ""},
		{"../", false, ""},
		{"./", false, ""},
		{".", false, ""},
		{"dollar$sign", true, "dollar$sign"},
		{"and&sign", true, "and&sign"},
		{"single`quote", true, "single`quote"},
		{"full:colon", true, "full:colon"},
		{"semi;colon", true, "semi;colon"},
		{"hash#tag", true, "hash#tag"},
		{"percent%sign", true, "percent%sign"},
		{"at@sign", true, "at@sign"},
		{"less<than", true, "less<than"},
		{"greater>than", true, "greater>than"},
		{"equal=to", true, "equal=to"},
		{"question?mark", true, "question?mark"},
		{"open[bracket", true, "open[bracket"},
		{"close]bracket", true, "close]bracket"},
		{"open{bracket", true, "open{bracket"},
		{"close}bracket", true, "close}bracket"},
		{"carrot^top", true, "carrot^top"},
		{"pipe|pipe", true, "pipe|pipe"},
		{"tilda~tilda", true, "tilda~tilda"},
		{"plus+sign", true, "plus+sign"},
		{"minus-sign", true, "minus-sign"},
		{"under_score", true, "under_score"},
		{"comma,comma", true, "comma,comma"},
		{"apostrophy's", true, "apostrophy's"},
		{`quotation"marks`, true, `quotation"marks`},
	}
	// If the OS is windows then the windows path is valid and will be updated
	if runtime.GOOS == "windows" {
		pathtests[0].valid = true
		pathtests[0].out = `some/windows/path`
	}

	// Test NewUploPath
	for _, pathtest := range pathtests {
		sp, err := NewUploPath(pathtest.in)
		// Verify expected Error
		if err != nil && pathtest.valid {
			t.Fatal("validateUplopath failed on valid path: ", pathtest.in)
		}
		if err == nil && !pathtest.valid {
			t.Fatal("validateUplopath succeeded on invalid path: ", pathtest.in)
		}
		// Verify expected path
		if err == nil && pathtest.valid && sp.String() != pathtest.out {
			t.Fatalf("Unexpected UploPath From New; got %v, expected %v, for test %v", sp.String(), pathtest.out, pathtest.in)
		}
	}

	// Test LoadString
	var sp UploPath
	for _, pathtest := range pathtests {
		err := sp.LoadString(pathtest.in)
		// Verify expected Error
		if err != nil && pathtest.valid {
			t.Fatal("validateUplopath failed on valid path: ", pathtest.in)
		}
		if err == nil && !pathtest.valid {
			t.Fatal("validateUplopath succeeded on invalid path: ", pathtest.in)
		}
		// Verify expected path
		if err == nil && pathtest.valid && sp.String() != pathtest.out {
			t.Fatalf("Unexpected UploPath from LoadString; got %v, expected %v, for test %v", sp.String(), pathtest.out, pathtest.in)
		}
	}

	// Test Join
	sp, err := NewUploPath("test")
	if err != nil {
		t.Fatal(err)
	}
	for _, pathtest := range pathtests {
		newUploPath, err := sp.Join(pathtest.in)
		// Verify expected Error
		if err != nil && pathtest.valid {
			t.Fatal("validateUplopath failed on valid path: ", pathtest.in)
		}
		if err == nil && !pathtest.valid {
			t.Fatal("validateUplopath succeeded on invalid path: ", pathtest.in)
		}
		// Verify expected path
		if err == nil && pathtest.valid && newUploPath.String() != "test/"+pathtest.out {
			t.Fatalf("Unexpected UploPath from Join; got %v, expected %v, for test %v", newUploPath.String(), "test/"+pathtest.out, pathtest.in)
		}
	}
}

// TestUplopathRebase tests the UploPath.Rebase method.
func TestUplopathRebase(t *testing.T) {
	var rebasetests = []struct {
		oldBase string
		newBase string
		uploPath string
		result  string
	}{
		{"a/b", "a", "a/b/myfile", "a/myfile"}, // basic rebase
		{"a/b", "", "a/b/myfile", "myfile"},    // newBase is root
		{"", "b", "myfile", "b/myfile"},        // oldBase is root
		{"a/a", "a/b", "a/a", "a/b"},           // folder == oldBase
	}

	for _, test := range rebasetests {
		var oldBase, newBase UploPath
		var err1, err2 error
		if test.oldBase == "" {
			oldBase = RootUploPath()
		} else {
			oldBase, err1 = newUploPath(test.oldBase)
		}
		if test.newBase == "" {
			newBase = RootUploPath()
		} else {
			newBase, err2 = newUploPath(test.newBase)
		}
		file, err3 := newUploPath(test.uploPath)
		expectedPath, err4 := newUploPath(test.result)
		if err := errors.Compose(err1, err2, err3, err4); err != nil {
			t.Fatal(err)
		}
		// Rebase the path
		res, err := file.Rebase(oldBase, newBase)
		if err != nil {
			t.Fatal(err)
		}
		// Check result.
		if !res.Equals(expectedPath) {
			t.Fatalf("'%v' doesn't match '%v'", res.String(), expectedPath.String())
		}
	}
}

// TestUplopathDir probes the Dir function for UploPaths.
func TestUplopathDir(t *testing.T) {
	var pathtests = []struct {
		path string
		dir  string
	}{
		{"one/dir", "one"},
		{"many/more/dirs", "many/more"},
		{"nodir", ""},
		{"/leadingslash", ""},
		{"./leadingdotslash", ""},
		{"", ""},
		{".", ""},
	}
	for _, pathtest := range pathtests {
		uploPath := UploPath{
			Path: pathtest.path,
		}
		dir, err := uploPath.Dir()
		if err != nil {
			t.Errorf("Dir should not return an error %v, path %v", err, pathtest.path)
			continue
		}
		if dir.Path != pathtest.dir {
			t.Errorf("Dir %v not the same as expected dir %v ", dir.Path, pathtest.dir)
			continue
		}
	}
}

// TestUplopathName probes the Name function for UploPaths.
func TestUplopathName(t *testing.T) {
	var pathtests = []struct {
		path string
		name string
	}{
		{"one/dir", "dir"},
		{"many/more/dirs", "dirs"},
		{"nodir", "nodir"},
		{"/leadingslash", "leadingslash"},
		{"./leadingdotslash", "leadingdotslash"},
		{"", ""},
		{".", ""},
	}
	for _, pathtest := range pathtests {
		uploPath := UploPath{
			Path: pathtest.path,
		}
		name := uploPath.Name()
		if name != pathtest.name {
			t.Errorf("name %v not the same as expected name %v ", name, pathtest.name)
		}
	}
}
