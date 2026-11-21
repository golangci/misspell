package main

import (
	"bytes"
	"cmp"
	_ "embed"
	"encoding/json"
	"go/format"
	"log"
	"os"
	"slices"
	"text/template"
)

//go:embed dict.go.tmpl
var dictTemplate string

type Tuple struct {
	Typo       string
	Correction string
}

type Info struct {
	Name    string
	Comment string
	Path    string
}

// regenerate words Go files from JSON files.
func main() {
	dictionaries := map[string]Info{
		"words.go": {
			Name:    "Main",
			Comment: "is the main rule set, not including locale-specific spellings",
			Path:    "internal/gen/sources/main.json",
		},
		"words_uk.go": {
			Name:    "British",
			Comment: "converts US spellings to UK spellings",
			Path:    "internal/gen/sources/uk.json",
		},
		"words_us.go": {
			Name:    "American",
			Comment: "converts UK spellings to US spellings",
			Path:    "internal/gen/sources/us.json",
		},
	}

	mainWords, err := read(dictionaries["words.go"].Path)
	if err != nil {
		log.Fatal(err)
	}

	for dest, src := range dictionaries {
		err := generate(src, dest, mainWords)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func generate(src Info, dest string, mainWords map[string][]string) error {
	data, err := read(src.Path)
	if err != nil {
		return err
	}

	tuples := toTuples(data)

	return write(tuples, src, dest, mainWords)
}

func toTuples(data map[string][]string) []Tuple {
	var tuples []Tuple

	for c, typos := range data {
		for _, typo := range typos {
			tuples = append(tuples, Tuple{Typo: typo, Correction: c})
		}
	}

	return tuples
}

func read(src string) (map[string][]string, error) {
	file, err := os.Open(src)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = file.Close()
	}()

	all := make(map[string][]string)

	err = json.NewDecoder(file).Decode(&all)
	if err != nil {
		return nil, err
	}

	return all, nil
}

func write(tuples []Tuple, src Info, dest string, mainWords map[string][]string) error {
	slices.SortStableFunc(tuples, func(a, b Tuple) int {
		if len(a.Typo) == len(b.Typo) {
			// if words are same size, then use
			// normal alphabetical order
			return cmp.Compare(a.Typo, b.Typo)
		}
		// INVERTED  -- biggest words first
		return cmp.Compare(len(b.Typo), len(a.Typo))
	})

	tmpl, err := template.New("words").Parse(dictTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer

	var groups []localeGroup
	if src.Name != "Main" {
		groups = localeGroups(tuples, mainWords)
	}

	err = tmpl.Execute(&buf, map[string]any{
		"Groups":  groups,
		"Name":    src.Name,
		"Comment": src.Comment,
		"Tuples":  tuples,
	})
	if err != nil {
		return err
	}

	source, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	words, err := os.Create(dest)
	if err != nil {
		return err
	}

	defer func() {
		_ = words.Close()
	}()

	_, err = words.Write(source)
	if err != nil {
		return err
	}

	return nil
}

type localeGroup struct {
	Tuple

	Prefix, PrefixCorrection string
	Typos                    []string
}

func localeGroups(regional []Tuple, mainWords map[string][]string) []localeGroup {
	mainCorrections := make(map[string]string)
	for _, t := range toTuples(mainWords) {
		mainCorrections[t.Typo] = t.Correction
	}

	var groups []localeGroup

	for _, t := range regional {
		typos := mainWords[t.Typo]
		if len(typos) == 0 {
			continue
		}

		group := localeGroup{Tuple: t, Typos: typos}
		// DictMain orders longer keys first. Record the stock prefix that
		// can win ahead of the regional rule when DictMain is added first.
		for end := len(t.Typo) - 1; end > 0; end-- {
			prefix := t.Typo[:end]
			if correction, ok := mainCorrections[prefix]; ok {
				group.Prefix, group.PrefixCorrection = prefix, correction
				break
			}
		}

		groups = append(groups, group)
	}

	return groups
}
