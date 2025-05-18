# Contributing

The files `words.go`, `works_uk.go`, and `works_us.go` must never be edited by hand.

## Adding a word

Misspell is neither a complete spell-checking program nor a grammar checker.
It is a tool to correct commonly misspelled English words.

The list of words must contain only common mistakes.

Before adding a word, you should have information about the misspelling frequency.

- [ ] more than 15k inside GitHub (this limit is arbitrary and can involve)
- [ ] don't exist inside the Wiktionary (as a modern form)
- [ ] don't exist inside the Cambridge Dictionary (as a modern form)
- [ ] don't exist inside the Oxford Dictionary (as a modern form)

If all criteria are met, a word can be added to the list of misspellings.

The word should be added to one of the following files.

- `cmd/gen/sources/main.json`: common words.
- `cmd/gen/sources/uk.json`: UK only words.
- `cmd/gen/sources/us.json`: US only words.

The PR description must provide all the information (links) about the misspelling frequency.
