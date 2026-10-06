// Package dictionary provides English WordNet definitions without network access.
package dictionary

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed wordnet.tar.gz
var archive []byte

type Sense struct {
	PartOfSpeech string   `json:"part_of_speech"`
	Definition   string   `json:"definition"`
	Synonyms     []string `json:"synonyms"`
}

type Entry struct {
	Word        string   `json:"word"`
	MatchedWord string   `json:"matched_word"`
	Senses      []*Sense `json:"senses"`
	Source      string   `json:"source"`
}

var once sync.Once
var loadError error
var words map[string][]*Sense
var exceptions map[string][]string

func Normalize(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 384 {
		return "", errors.New("Enter a word or short phrase (up to 96 characters)")
	}
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "’", "'"))
	value = strings.Join(strings.Fields(value), " ")
	if value == "" || utf8.RuneCountInString(value) > 96 || len(strings.Fields(value)) > 6 {
		return "", errors.New("Enter a word or short phrase (up to 96 characters)")
	}
	hasLetter := false
	for _, r := range value {
		if unicode.IsLetter(r) {
			hasLetter = true
			continue
		}
		if !unicode.IsNumber(r) && r != ' ' && r != '-' && r != '\'' {
			return "", errors.New("Use letters, numbers, spaces, apostrophes or hyphens")
		}
	}
	if !hasLetter {
		return "", errors.New("Enter a word containing letters")
	}
	return value, nil
}

func Load() error { once.Do(func() { loadError = load() }); return loadError }

func Lookup(value string) (Entry, error) {
	word, err := Normalize(value)
	if err != nil {
		return Entry{}, err
	}
	if err := Load(); err != nil {
		return Entry{}, err
	}
	entry := Entry{Word: word, MatchedWord: word, Senses: []*Sense{}, Source: "WordNet 3.1"}
	candidates := append([]string{word}, exceptions[word]...)
	// WordNet morphology rules supplement the exception lists.
	for _, rule := range [][2]string{{"s", ""}, {"ses", "s"}, {"xes", "x"}, {"zes", "z"}, {"ches", "ch"}, {"shes", "sh"}, {"men", "man"}, {"ies", "y"}, {"es", "e"}, {"ed", "e"}, {"ed", ""}, {"ing", "e"}, {"ing", ""}, {"er", ""}, {"est", ""}, {"er", "e"}, {"est", "e"}} {
		if strings.HasSuffix(word, rule[0]) {
			candidates = append(candidates, strings.TrimSuffix(word, rule[0])+rule[1])
		}
	}
	for _, candidate := range candidates {
		if senses := words[candidate]; len(senses) > 0 {
			entry.MatchedWord = candidate
			entry.Senses = senses
			break
		}
	}
	return entry, nil
}

func load() error {
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer reader.Close()
	files := map[string][]byte{}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return err
		}
		files[header.Name] = data
	}
	words = map[string][]*Sense{}
	exceptions = map[string][]string{}
	parts := map[string]string{"noun": "noun", "verb": "verb", "adj": "adjective", "adv": "adverb"}
	for _, pos := range []string{"noun", "verb", "adj", "adv"} {
		if len(files["data."+pos]) == 0 || len(files["index."+pos]) == 0 || len(files[pos+".exc"]) == 0 {
			return fmt.Errorf("missing WordNet %s data", pos)
		}
		senses := map[string]*Sense{}
		scanner := bufio.NewScanner(bytes.NewReader(files["data."+pos]))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, " ") {
				continue
			}
			header, gloss, ok := strings.Cut(line, " | ")
			if !ok {
				return fmt.Errorf("invalid WordNet %s record", pos)
			}
			fields := strings.Fields(header)
			if len(fields) < 4 {
				return errors.New("invalid WordNet record")
			}
			count, err := strconv.ParseInt(fields[3], 16, 32)
			if err != nil || len(fields) < 4+2*int(count) {
				return errors.New("invalid WordNet word count")
			}
			sense := &Sense{PartOfSpeech: parts[pos], Definition: strings.TrimSpace(gloss), Synonyms: []string{}}
			for i := 0; i < int(count); i++ {
				lemma := fields[4+2*i]
				for _, suffix := range []string{"(a)", "(p)", "(ip)"} {
					lemma = strings.TrimSuffix(lemma, suffix)
				}
				sense.Synonyms = append(sense.Synonyms, strings.ReplaceAll(lemma, "_", " "))
			}
			senses[fields[0]] = sense
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		// Index offsets retain WordNet's order of senses for each word.
		scanner = bufio.NewScanner(bytes.NewReader(files["index."+pos]))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, " ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 6 {
				return errors.New("invalid WordNet index")
			}
			count, err := strconv.Atoi(fields[2])
			if err != nil || count < 1 || len(fields) < count+6 {
				return errors.New("invalid WordNet sense count")
			}
			word := strings.ToLower(strings.ReplaceAll(fields[0], "_", " "))
			for _, offset := range fields[len(fields)-count:] {
				sense := senses[offset]
				if sense == nil {
					return errors.New("missing WordNet sense")
				}
				words[word] = append(words[word], sense)
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		scanner = bufio.NewScanner(bytes.NewReader(files[pos+".exc"]))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			for _, base := range fields[1:] {
				exceptions[strings.ReplaceAll(fields[0], "_", " ")] = append(exceptions[strings.ReplaceAll(fields[0], "_", " ")], strings.ReplaceAll(base, "_", " "))
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	if len(words) < 140000 {
		return errors.New("incomplete WordNet dictionary")
	}
	return nil
}
