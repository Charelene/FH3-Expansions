package main

// namesolve learns a per-character name substitution between a copy of the
// game's media folder whose file and folder names were encoded and a copy with
// the real names (the Xbox game's Mount\media), from the two file lists alone.
// No table is built in. setup uses it when the Windows base game's media does
// not carry real names; the command namesolve prints and writes the table.
//
// 1. Files whose size occurs exactly once on each side and whose paths have the
//    same shape (number of components, length of each) are paired; every
//    character position of such a pair is a vote "encoded c is real p".
// 2. Votes are resolved one to one, strongest first.
// 3. Names that are still partly unknown are matched against the real names in
//    the decoded parent folder: a single candidate that agrees with every known
//    character (and maps unknown characters only to unused real ones) adds
//    votes. Steps 2 and 3 repeat until nothing changes.
//
// It solves any one-to-one character substitution. A scheme that renames files
// at random is not solvable this way.

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() { devCommands["namesolve"] = cmdNameSolve }

type nsFile struct {
	parts []string
	size  int64
}

func nsList(root string, lower bool) ([]nsFile, error) {
	var out []nsFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if lower {
			rel = strings.ToLower(rel)
		}
		out = append(out, nsFile{strings.Split(rel, "/"), info.Size()})
		return nil
	})
	return out, err
}

func nsSameShape(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len([]rune(a[i])) != len([]rune(b[i])) {
			return false
		}
	}
	return true
}

type nsSolver struct {
	votes map[rune]map[rune]int
	m     map[rune]rune // encoded -> real
	used  map[rune]rune // real -> encoded
}

func (s *nsSolver) vote(a, b string, w int) {
	ra, rb := []rune(a), []rune(b)
	for i := range ra {
		v := s.votes[ra[i]]
		if v == nil {
			v = map[rune]int{}
			s.votes[ra[i]] = v
		}
		v[rb[i]] += w
	}
}

// resolve turns the votes into a one-to-one map, strongest vote first.
func (s *nsSolver) resolve() {
	type cand struct {
		from, to rune
		n        int
	}
	var cs []cand
	for f, v := range s.votes {
		for t, n := range v {
			cs = append(cs, cand{f, t, n})
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].n != cs[j].n {
			return cs[i].n > cs[j].n
		}
		if cs[i].from != cs[j].from {
			return cs[i].from < cs[j].from
		}
		return cs[i].to < cs[j].to
	})
	s.m, s.used = map[rune]rune{}, map[rune]rune{}
	for _, c := range cs {
		if _, ok := s.m[c.from]; ok {
			continue
		}
		if _, ok := s.used[c.to]; ok {
			continue
		}
		s.m[c.from], s.used[c.to] = c.to, c.from
	}
}

// decode returns the decoded name and whether every character was known.
func (s *nsSolver) decode(name string) (string, bool) {
	var b strings.Builder
	ok := true
	for _, c := range name {
		if p, k := s.m[c]; k {
			b.WriteRune(p)
		} else {
			b.WriteRune('?')
			ok = false
		}
	}
	return b.String(), ok
}

// fits says whether the encoded name can decode to the real name under the
// current map, with unknown characters taking only real characters nobody uses.
func (s *nsSolver) fits(enc, real string) bool {
	rs, rr := []rune(enc), []rune(real)
	if len(rs) != len(rr) {
		return false
	}
	tmp := map[rune]rune{}
	tmpUsed := map[rune]rune{}
	for i, c := range rs {
		if p, k := s.m[c]; k {
			if p != rr[i] {
				return false
			}
			continue
		}
		if p, k := tmp[c]; k {
			if p != rr[i] {
				return false
			}
			continue
		}
		if _, k := s.used[rr[i]]; k {
			return false
		}
		if o, k := tmpUsed[rr[i]]; k && o != c {
			return false
		}
		tmp[c], tmpUsed[rr[i]] = rr[i], c
	}
	return true
}

// nameSolution is what solveNames learned.
type nameSolution struct {
	table   map[rune]rune // encoded -> real
	votes   map[rune]map[rune]int
	files   int // files on the encoded side
	decoded int // of those, names fully decoded
	found   int // of those, present under the same path on the real side
	unknown map[rune]int
}

// solveNames learns the substitution that turns the names under encRoot into
// the names under realRoot. Progress lines go to log (may be io.Discard).
func solveNames(encRoot, realRoot string, minSize int64, log io.Writer) (*nameSolution, error) {
	enc, err := nsList(encRoot, false)
	if err != nil {
		return nil, err
	}
	real, err := nsList(realRoot, true)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(log, "files: %d encoded, %d real\n", len(enc), len(real))

	s := &nsSolver{votes: map[rune]map[rune]int{}}

	// 1. unique sizes with the same path shape
	bySize := map[int64][]nsFile{}
	for _, f := range real {
		bySize[f.size] = append(bySize[f.size], f)
	}
	encBySize := map[int64]int{}
	for _, f := range enc {
		encBySize[f.size]++
	}
	pairs := 0
	for _, f := range enc {
		if f.size < minSize || encBySize[f.size] != 1 {
			continue
		}
		c := bySize[f.size]
		if len(c) != 1 || !nsSameShape(f.parts, c[0].parts) {
			continue
		}
		pairs++
		for i := range f.parts {
			s.vote(f.parts[i], c[0].parts[i], 1)
		}
	}
	fmt.Fprintf(log, "pass 1: %d files paired by unique size\n", pairs)
	s.resolve()

	// 3. names against the real names of their decoded folder
	children := map[string]map[string]bool{}
	for _, f := range real {
		for i := range f.parts {
			dir := strings.Join(f.parts[:i], "/")
			if children[dir] == nil {
				children[dir] = map[string]bool{}
			}
			children[dir][f.parts[i]] = true
		}
	}
	for round := 1; round <= 20; round++ {
		added := 0
		seen := map[string]bool{}
		for _, f := range enc {
			var dir []string
			for i, part := range f.parts {
				dec, ok := s.decode(part)
				if !ok {
					key := strings.Join(dir, "/") + "\x00" + part
					if seen[key] {
						break
					}
					seen[key] = true
					var hit string
					n := 0
					for cand := range children[strings.Join(dir, "/")] {
						if s.fits(part, cand) {
							hit = cand
							n++
						}
					}
					if n == 1 {
						s.vote(part, hit, 1)
						added++
						dec = hit
					} else {
						break
					}
				}
				if i < len(f.parts)-1 {
					dir = append(dir, dec)
				}
			}
		}
		before := len(s.m)
		s.resolve()
		fmt.Fprintf(log, "pass 3, round %d: %d names matched in their folder, %d characters known (was %d)\n", round, added, len(s.m), before)
		if added == 0 || len(s.m) == before {
			break
		}
	}

	sol := &nameSolution{table: s.m, votes: s.votes, files: len(enc), unknown: map[rune]int{}}
	realSet := map[string]bool{}
	for _, f := range real {
		realSet[strings.Join(f.parts, "/")] = true
	}
	for _, f := range enc {
		var parts []string
		ok := true
		for _, p := range f.parts {
			d, k := s.decode(p)
			if !k {
				ok = false
				for _, c := range p {
					if _, kk := s.m[c]; !kk {
						sol.unknown[c]++
					}
				}
			}
			parts = append(parts, d)
		}
		if ok {
			sol.decoded++
			if realSet[strings.ToLower(strings.Join(parts, "/"))] {
				sol.found++
			}
		}
	}
	fmt.Fprintf(log, "names fully decoded: %d of %d; of those %d exist under the same path on the real side\n", sol.decoded, sol.files, sol.found)
	return sol, nil
}

func cmdNameSolve(args []string) error {
	set := flag.NewFlagSet("namesolve", flag.ContinueOnError)
	minSize := set.Int64("minsize", 4096, "only files at least this large pair by size")
	write := set.String("write", "", "write the learned table here (encoded<TAB>real per line)")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 2 {
		return fmt.Errorf("usage: namesolve [-minsize n] [-write table.tsv] <encoded media folder> <real media folder>")
	}
	sol, err := solveNames(set.Arg(0), set.Arg(1), *minSize, os.Stdout)
	if err != nil {
		return err
	}
	keys := make([]rune, 0, len(sol.table))
	for k := range sol.table {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return sol.table[keys[i]] < sol.table[keys[j]] })
	fmt.Println("table (encoded -> real, votes for the winner / all votes):")
	for _, k := range keys {
		total := 0
		for _, n := range sol.votes[k] {
			total += n
		}
		fmt.Printf("  %q -> %q  %d/%d\n", k, sol.table[k], sol.votes[k][sol.table[k]], total)
	}
	if len(sol.unknown) > 0 {
		fmt.Print("characters still unknown (occurrences):")
		for c, n := range sol.unknown {
			fmt.Printf(" %q=%d", c, n)
		}
		fmt.Println()
	}
	if *write != "" {
		f, err := os.Create(*write)
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		for _, k := range keys {
			fmt.Fprintf(w, "%s\t%s\n", string(k), string(sol.table[k]))
		}
		if err := w.Flush(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Println("table written to", *write)
	}
	return nil
}
