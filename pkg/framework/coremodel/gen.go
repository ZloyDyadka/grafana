// go:build ignore
//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/grafana/cuetsy"
	gcgen "github.com/grafana/grafana/pkg/codegen"
	"github.com/grafana/thema"
)

var lib = thema.NewLibrary(cuecontext.New())

const sep = string(filepath.Separator)

// Generate Go and Typescript implementations for all coremodels, and populate the
// coremodel static registry.
func main() {
	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "coremodel code generator does not currently accept any arguments\n, got %q", os.Args)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get working directory: %s", err)
		os.Exit(1)
	}

	// TODO this binds us to only having coremodels in a single directory. If we need more, compgen is the way
	grootp := strings.Split(cwd, sep)
	groot := filepath.Join(sep, filepath.Join(grootp[:len(grootp)-3]...))

	cmroot := filepath.Join(groot, "pkg", "coremodel")
	tsroot := filepath.Join(groot, "packages", "grafana-schema", "src", "schema")

	items, err := ioutil.ReadDir(cmroot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read coremodels parent dir %s: %s\n", cmroot, err)
		os.Exit(1)
	}

	var lins []*gcgen.ExtractedLineage
	for _, item := range items {
		if item.IsDir() {
			lin, err := gcgen.ExtractLineage(filepath.Join(cmroot, item.Name(), "coremodel.cue"), lib)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not process coremodel dir %s: %s\n", filepath.Join(cmroot, item.Name()), err)
				os.Exit(1)
			}

			lins = append(lins, lin)
		}
	}
	sort.Slice(lins, func(i, j int) bool {
		return lins[i].Lineage.Name() < lins[j].Lineage.Name()
	})

	wd := gcgen.NewWriteDiffer()
	for _, ls := range lins {
		gofiles, err := ls.GenerateGoCoremodel(filepath.Join(cmroot, ls.Lineage.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate Go for %s: %s\n", ls.Lineage.Name(), err)
			os.Exit(1)
		}
		wd.Merge(gofiles)

		tsfiles, err := ls.GenerateTypescriptCoremodel(filepath.Join(tsroot, ls.Lineage.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate TypeScript for %s: %s\n", ls.Lineage.Name(), err)
			os.Exit(1)
		}
		wd.Merge(tsfiles)
	}

	regfiles, err := gcgen.GenerateCoremodelRegistry(filepath.Join(groot, "pkg", "framework", "coremodel", "registry", "registry_gen.go"), lins)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate coremodel registry: %s\n", err)
		os.Exit(1)
	}
	wd.Merge(regfiles)

	// TODO generating these is here temporarily until we make a more permanent home
	wdsh, err := genSharedSchemas(groot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TS gen error for shared schemas in %s: %w", filepath.Join(groot, "packages", "grafana-schema", "src", "schema"), err)
		os.Exit(1)
	}
	wd.Merge(wdsh)

	if _, set := os.LookupEnv("CODEGEN_VERIFY"); set {
		err = wd.Verify()
		if err != nil {
			fmt.Fprintf(os.Stderr, "generated code is not up to date:\n%s\nrun `make gen-cue` to regenerate\n\n", err)
			os.Exit(1)
		}
	} else {
		err = wd.Write()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error while writing generated code to disk:\n%s\n", err)
			os.Exit(1)
		}
	}
}
func genSharedSchemas(groot string) (gcgen.WriteDiffer, error) {
	abspath := filepath.Join(groot, "packages", "grafana-schema", "src", "schema")
	cfg := &load.Config{
		ModuleRoot: groot,
		Module:     "github.com/grafana/grafana",
		Dir:        abspath,
	}

	bi := load.Instances(nil, cfg)
	if len(bi) > 1 {
		return nil, fmt.Errorf("loading CUE files in %s resulted in more than one instance", abspath)
	}

	ctx := cuecontext.New()
	v := ctx.BuildInstance(bi[0])
	if v.Err() != nil {
		return nil, fmt.Errorf("errors while building CUE in %s: %s", abspath, v.Err())
	}

	b, err := cuetsy.Generate(v, cuetsy.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TS: %w", err)
	}

	wd := gcgen.NewWriteDiffer()
	wd[filepath.Join(abspath, "mudball.gen.ts")] = append([]byte(`//~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
// This file is autogenerated. DO NOT EDIT.
//
// To regenerate, run "make gen-cue" from the repository root.
//~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
`), b...)
	return wd, nil
}
