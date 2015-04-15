// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// rewrite contains commands for writing the altered import statements.
package rewrite

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type ListStatus byte

func (ls ListStatus) String() string {
	switch ls {
	case StatusUnknown:
		return "?"
	case StatusMissing:
		return "m"
	case StatusStd:
		return "s"
	case StatusLocal:
		return "l"
	case StatusExternal:
		return "e"
	case StatusInternal:
		return "i"
	case StatusUnused:
		return "u"
	}
	return ""
}

const (
	StatusUnknown ListStatus = iota
	StatusMissing
	StatusStd
	StatusLocal
	StatusExternal
	StatusInternal
	StatusUnused
)

type ListItem struct {
	Status ListStatus
	Path   string
}

func (li ListItem) String() string {
	return li.Status.String() + " " + li.Path
}

type ListItemSort []ListItem

func (li ListItemSort) Len() int      { return len(li) }
func (li ListItemSort) Swap(i, j int) { li[i], li[j] = li[j], li[i] }
func (li ListItemSort) Less(i, j int) bool {
	if li[i].Status == li[j].Status {
		return strings.Compare(li[i].Path, li[j].Path) < 0
	}
	return li[i].Status > li[j].Status
}

const (
	vendorFilename = "vendor.json"
	internalFolder = "internal"
	toolName       = "github.com/kardianos/vendor"
)

var (
	internalVendor      = filepath.Join(internalFolder, vendorFilename)
	internalFolderSlash = string(filepath.Separator) + internalFolder + string(filepath.Separator)
)

var (
	ErrVendorFileExists  = errors.New(internalVendor + " file already exists.")
	ErrMissingVendorFile = errors.New("Unable to find internal folder with vendor file.")
	ErrMissingGOROOT     = errors.New("Unable to determine GOROOT.")
	ErrMissingGOPATH     = errors.New("Missing GOPATH.")
	ErrVendorExists      = errors.New("Package already exists as a vendor package.")
	ErrLocalPackage      = errors.New("Cannot vendor a local package.")
)

type ErrNotInGOPATH struct {
	Missing string
}

func (err ErrNotInGOPATH) Error() string {
	return fmt.Sprintf("Package %q not in GOPATH.", err.Missing)
}

func CmdInit() error {
	/*
		1. Determine if CWD contains "internal/vendor.json".
		2. If exists, return error.
		3. Create directory if it doesn't exist.
		4. Create "internal/vendor.json" file.
	*/
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(wd, internalVendor))
	if os.IsNotExist(err) == false {
		return ErrVendorFileExists
	}
	err = os.MkdirAll(filepath.Join(wd, internalFolder), 0777)
	if err != nil {
		return err
	}
	vf := &VendorFile{
		Tool: toolName,
	}
	return writeVendorFile(wd, vf)
}

func CmdList() ([]ListItem, error) {
	/*
		1. Find vendor root.
		2. Find vendor root import path via GOPATH.
		3. Walk directory, find all directories with go files.
		4. Parse imports for all go files.
		5. Determine the status of all imports.
		  * Std
		  * Local
		  * External Vendor
		  * Internal Vendor
		  * Unused Vendor
		6. Return Vendor import paths.
	*/
	ctx, err := NewContextWD()
	if err != nil {
		return nil, err
	}

	err = ctx.LoadPackage()
	if err != nil {
		return nil, err
	}

	li := make([]ListItem, 0, len(ctx.Package))
	for _, pkg := range ctx.Package {
		li = append(li, ListItem{Status: pkg.Status, Path: pkg.ImportPath})
	}
	// Sort li by Status, then Path.
	sort.Sort(ListItemSort(li))

	return li, nil
}

/*
	Add, Update, and Remove will start with the same steps as List.
	Rather then returning the results, it will find any affected files,
	alter their imports, then write the files back out. Also copy or remove
	files and folders as needed.
*/

func CmdAdd(importPath string) error {
	importPath = slashToImportPath(importPath)
	ctx, err := NewContextWD()
	if err != nil {
		return err
	}

	err = ctx.LoadPackage(importPath)
	if err != nil {
		return err
	}

	pkg := ctx.Package[importPath]
	if pkg.Status != StatusExternal {
		if pkg.Status == StatusInternal {
			return ErrVendorExists
		}
		if pkg.Status == StatusLocal {
			return ErrLocalPackage
		}
		return ErrNotInGOPATH{importPath}
	}

	// Determine correct local import path (from GOPATH).
	/*
		"crypto/tls" -> "path/to/mypkg/internal/crypto/tls"
		"yours/internal/yourpkg" -> "path/to/mypkg/internal/yourpkg"
		"github.com/kardianos/osext" -> "patn/to/mypkg/internal/github.com/kardianos/osext"
	*/
	// The following method "cheats" and doesn't look at any external vendor file.
	ss := strings.Split(importPath, internalFolderSlash)
	localImportPath := path.Join(ctx.RootImportPath, internalFolder, ss[len(ss)-1])

	// Update vendor file with correct Local field.
	// TODO: find the Version and VersionTime.
	ctx.VendorFile.Package = append(ctx.VendorFile.Package, &VendorPackage{
		Vendor: importPath,
		Local:  localImportPath,
	})
	err = writeVendorFile(ctx.RootDir, ctx.VendorFile)
	if err != nil {
		return err
	}

	err = CopyPackage(pkg.Dir, filepath.Join(ctx.RootGopath, slashToFilepath(localImportPath)))
	if err != nil {
		return err
	}

	err = ctx.LoadPackage(importPath)
	if err != nil {
		return err
	}

	files := ctx.fileImports[importPath]

	// Determine which files to touch.
	err = RewriteFiles(files, []Rule{Rule{From: importPath, To: localImportPath}})
	if err != nil {
		return err
	}

	return nil
}
func CmdUpdate(importPath string) error {
	return nil
}
func CmdRemove(importPath string) error {
	return nil
}
