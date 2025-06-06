// The misspell command corrects commonly misspelled English words in source files.
package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/golangci/misspell"
)

const (
	outputFormatCSV     = "csv"
	outputFormatSQLite  = "sqlite"
	outputFormatSQLite3 = "sqlite3"
)

const (
	// Note for gometalinter it must be "File:Line:Column: Msg"
	//  note space between ": Msg"
	defaultWriteTmpl = `{{ .Filename }}:{{ .Line }}:{{ .Column }}: corrected "{{ .Original }}" to "{{ .Corrected }}"`
	defaultReadTmpl  = `{{ .Filename }}:{{ .Line }}:{{ .Column }}: "{{ .Original }}" is a misspelling of "{{ .Corrected }}"`
	csvTmpl          = `{{ printf "%q" .Filename }},{{ .Line }},{{ .Column }},{{ .Original }},{{ .Corrected }}`
	csvHeader        = `file,line,column,typo,corrected`
	sqliteTmpl       = `INSERT INTO misspell VALUES({{ printf "%q" .Filename }},{{ .Line }},{{ .Column }},{{ printf "%q" .Original }},{{ printf "%q" .Corrected }});`
	sqliteHeader     = `PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE misspell(
	"file" TEXT, "line" INTEGER, "column" INTEGER, "typo" TEXT, "corrected" TEXT
);`
	sqliteFooter = "COMMIT;"
)

var version = "dev"

var (
	output *log.Logger
	debug  *log.Logger
)

var (
	defaultWrite *template.Template
	defaultRead  *template.Template
)

//nolint:funlen,nestif,gocognit,gocyclo,maintidx // TODO(ldez) must be fixed.
func main() {
	t := time.Now()

	var (
		workers      = flag.Int("j", 0, "Number of workers, 0 = number of CPUs")
		writeit      = flag.Bool("w", false, "Overwrite file with corrections (default is just to display)")
		quietFlag    = flag.Bool("q", false, "Do not emit misspelling output")
		outFlag      = flag.String("o", "stdout", "output file or [stderr|stdout|]")
		format       = flag.String("f", "", "'csv', 'sqlite3' or custom Golang template for output")
		ignores      = flag.String("i", "", "ignore the following corrections, comma-separated")
		userDictPath = flag.String("dict", "", "User defined corrections file path (.csv). CSV format: typo,fix")
		locale       = flag.String("locale", "", "Correct spellings using locale preferences for US or UK.  Default is to use a neutral variety of English.  Setting locale to US will correct the British spelling of 'colour' to 'color'")
		mode         = flag.String("source", "text", "Source mode: text (default), go (comments only)")
		debugFlag    = flag.Bool("debug", false, "Debug matching, very slow")
		exitError    = flag.Bool("error", false, "Exit with 2 if misspelling found")
		showVersion  = flag.Bool("v", false, "Show version and exit")

		showLegal = flag.Bool("legal", false, "Show legal information and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *showLegal {
		fmt.Println(misspell.Legal)
		return
	}

	//
	// Number of Workers / CPU to use
	//
	if *workers < 0 {
		log.Fatalf("-j must >= 0")
	}
	if *workers == 0 {
		*workers = runtime.NumCPU()
	}
	if *debugFlag {
		*workers = 1
	}

	//
	// Source input mode
	//
	switch *mode {
	case "auto", "go", "text":
	default:
		log.Fatalf("Mode must be one of auto=guess, go=golang source, text=plain or markdown-like text")
	}

	debug = newDebugLogger(*debugFlag)

	r := misspell.Replacer{
		Replacements: misspell.DictMain,
		Debug:        *debugFlag,
	}

	//
	// Figure out regional variations
	//
	switch strings.ToUpper(*locale) {
	case "":
		// nothing
	case "US":
		r.AddRuleList(misspell.DictAmerican)
	case "UK", "GB":
		r.AddRuleList(misspell.DictBritish)
	case "NZ", "AU", "CA":
		log.Fatalf("Help wanted.")
	default:
		log.Fatalf("Unknown locale: %q", *locale)
	}

	//
	// Load user defined words
	//
	if *userDictPath != "" {
		userDict, err := readUserDict(*userDictPath)
		if err != nil {
			log.Fatalf("reading user defined corrections: %v", err)
		}

		r.AddRuleList(userDict)
	}

	//
	// Stuff to ignore
	//
	if *ignores != "" {
		r.RemoveRule(strings.Split(*ignores, ","))
	}

	//
	// Output logger
	//
	var cleanup func() error
	output, cleanup = newLogger(*quietFlag, *outFlag)
	defer func() { _ = cleanup() }()

	//
	// Custom output format
	//
	var err error
	defaultWrite, defaultRead, err = createTemplates(*format)
	if err != nil {
		log.Fatal(err)
	}

	switch *format {
	case outputFormatCSV:
		output.Println(csvHeader)
	case outputFormatSQLite, outputFormatSQLite3:
		output.Println(sqliteHeader)
	}

	// Done with Flags.
	// Compile the Replacer and process files
	r.Compile()

	args := flag.Args()
	debug.Printf("initialization complete in %v", time.Since(t))

	// stdin/stdout
	if len(args) == 0 {
		// If we are working with pipes/stdin/stdout there is no concurrency,
		// so we can directly send data to the writers.
		var fileOut io.Writer
		var errOut io.Writer
		switch *writeit {
		case true:
			// If we are writing the corrected stream,
			// the corrected stream goes to stdout,
			// and the misspelling errors goes to stderr,
			// so we can do something like this:
			//    curl something | misspell -w | gzip > afile.gz
			fileOut = os.Stdout
			errOut = os.Stderr
		case false:
			// If we are not writing out the corrected stream then work just like files.
			// Misspelling errors are sent to stdout.
			fileOut = io.Discard
			errOut = os.Stdout
		}

		count := 0
		next := func(diff misspell.Diff) {
			count++

			// don't even evaluate the output templates
			if *quietFlag {
				return
			}

			diff.Filename = "stdin"

			if *writeit {
				defaultWrite.Execute(errOut, diff)
			} else {
				defaultRead.Execute(errOut, diff)
			}

			errOut.Write([]byte{'\n'})
		}

		err := r.ReplaceReader(os.Stdin, fileOut, next)
		if err != nil {
			log.Fatal(err)
		}

		switch *format {
		case outputFormatSQLite, outputFormatSQLite3:
			fileOut.Write([]byte(sqliteFooter))
		}

		if count != 0 && *exitError {
			// error
			os.Exit(2)
		}

		return
	}

	c := make(chan string, 64)
	results := make(chan int, *workers)

	for range *workers {
		go worker(*writeit, &r, *mode, c, results)
	}

	for _, filename := range args {
		filepath.Walk(filename, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				c <- path
			}
			return nil
		})
	}
	close(c)

	count := 0
	for range *workers {
		changed := <-results
		count += changed
	}

	switch *format {
	case outputFormatSQLite, outputFormatSQLite3:
		output.Println(sqliteFooter)
	}

	if count != 0 && *exitError {
		os.Exit(2)
	}
}

func worker(writeit bool, r *misspell.Replacer, mode string, files <-chan string, results chan<- int) {
	count := 0
	for filename := range files {
		orig, err := misspell.ReadTextFile(filename)
		if err != nil {
			log.Println(err)
			continue
		}

		if orig == "" {
			continue
		}

		debug.Printf("Processing %s", filename)

		var updated string
		var changes []misspell.Diff

		if mode == "go" {
			updated, changes = r.ReplaceGo(orig)
		} else {
			updated, changes = r.Replace(orig)
		}

		if len(changes) == 0 {
			continue
		}

		count += len(changes)

		for _, diff := range changes {
			// add in filename
			diff.Filename = filename

			// Output can be done by doing multiple goroutines
			// and can clobber os.Stdout.
			//
			// the log package can be used simultaneously from multiple goroutines
			var buffer bytes.Buffer
			if writeit {
				defaultWrite.Execute(&buffer, diff)
			} else {
				defaultRead.Execute(&buffer, diff)
			}

			// goroutine-safe print to os.Stdout
			output.Println(buffer.String())
		}

		if writeit {
			os.WriteFile(filename, []byte(updated), 0)
		}
	}
	results <- count
}

func readUserDict(userDictPath string) ([]string, error) {
	file, err := os.Open(userDictPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load user defined corrections %q: %w", userDictPath, err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = 2

	data, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading user defined corrections: %w", err)
	}

	var userDict []string
	for _, row := range data {
		userDict = append(userDict, row...)
	}

	return userDict, nil
}

func createTemplates(format string) (writeTmpl, readTmpl *template.Template, err error) {
	switch {
	case format == outputFormatCSV:
		tmpl := template.Must(template.New(outputFormatCSV).Parse(csvTmpl))
		return tmpl, tmpl, nil

	case format == outputFormatSQLite || format == outputFormatSQLite3:
		tmpl := template.Must(template.New(outputFormatSQLite3).Parse(sqliteTmpl))
		return tmpl, tmpl, nil

	case format != "":
		tmpl, err := template.New("custom").Parse(format)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to compile log format: %w", err)
		}
		return tmpl, tmpl, nil

	default: // format == ""
		writeTmpl = template.Must(template.New("defaultWrite").Parse(defaultWriteTmpl))
		readTmpl = template.Must(template.New("defaultRead").Parse(defaultReadTmpl))
		return
	}
}

func newLogger(quiet bool, outputPath string) (logger *log.Logger, cleanup func() error) {
	// We can't just write to os.Stdout directly
	// since we have multiple goroutine all writing at the same time causing broken output.
	// Log is routine safe.
	// We see it, so it doesn't use a prefix or include a time stamp.
	switch {
	case quiet || outputPath == os.DevNull:
		logger = log.New(io.Discard, "", 0)
	case outputPath == "/dev/stderr" || outputPath == "stderr":
		logger = log.New(os.Stderr, "", 0)
	case outputPath == "/dev/stdout" || outputPath == "stdout":
		logger = log.New(os.Stdout, "", 0)
	case outputPath == "" || outputPath == "-":
		logger = log.New(os.Stdout, "", 0)
	default:
		fo, err := os.Create(outputPath)
		if err != nil {
			log.Fatalf("unable to create outfile %q: %s", outputPath, err)
		}
		return log.New(fo, "", 0), fo.Close
	}

	return logger, func() error { return nil }
}

func newDebugLogger(enable bool) *log.Logger {
	if enable {
		return log.New(os.Stderr, "DEBUG ", 0)
	}

	return log.New(io.Discard, "", 0)
}
