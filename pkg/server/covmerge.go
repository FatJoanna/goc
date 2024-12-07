package server

import (
	"errors"
	"fmt"
	"goc/pkg/log"
	"golang.org/x/tools/cover"
	"io"
	"sort"
)

// MergeProfiles merges two coverage profiles.
// The profiles are expected to be similar - that is, from multiple invocations of a
// single binary, or multiple binaries using the same codebase.
// In particular, any source files with the same path must have had identical content
// when building the binaries.
// MergeProfiles expects its arguments to be sorted: Profiles in alphabetical order,
// and lines in files in the order those lines appear. These are standard constraints for
// Go coverage profiles. The resulting profile will also obey these constraints.
func MergeProfiles(a []*cover.Profile, b []*cover.Profile) ([]*cover.Profile, error) {
	var result []*cover.Profile
	files := make(map[string]*cover.Profile, len(a))
	for _, profile := range a {
		np := deepCopyProfile(*profile)
		result = append(result, &np)
		files[np.FileName] = &np
	}

	needsSort := false
	// Now merge b into the result
	for _, profile := range b {
		dest, ok := files[profile.FileName]
		if ok {
			if err := ensureProfilesMatch(profile, dest); err != nil {
				// 如果某个文件中存在不一致之处，则以新文件为准
				log.Errorf("error merging %s: %v", profile.FileName, err)
				continue
			}
			for i, block := range profile.Blocks {
				db := &dest.Blocks[i]
				db.Count += block.Count
			}
		} else {
			// If we get some file we haven't seen before, we just append it.
			// We need to sort this later to ensure the resulting profile is still correctly sorted.
			np := deepCopyProfile(*profile)
			files[np.FileName] = &np
			result = append(result, &np)
			needsSort = true
		}
	}
	if needsSort {
		sort.Slice(result, func(i, j int) bool { return result[i].FileName < result[j].FileName })
	}
	return result, nil
}

// MergeMultipleProfiles merges more than two profiles together.
// MergeMultipleProfiles is equivalent to calling MergeProfiles on pairs of profiles
// until only one profile remains.
func MergeMultipleProfiles(profiles [][]*cover.Profile) ([]*cover.Profile, error) {
	if len(profiles) < 1 {
		return nil, errors.New("can't merge zero profiles")
	}
	result := profiles[0]
	for _, profile := range profiles[1:] {
		var err error
		if result, err = MergeProfiles(result, profile); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// DumpProfile dumps the profiles given to writer in go coverage format.
func DumpProfile(profiles []*cover.Profile, writer io.Writer) error {
	if len(profiles) == 0 {
		return errors.New("can't write an empty profile")
	}
	if _, err := io.WriteString(writer, "mode: "+profiles[0].Mode+"\n"); err != nil {
		return err
	}
	for _, profile := range profiles {
		for _, block := range profile.Blocks {
			if _, err := fmt.Fprintf(writer, "%s:%d.%d,%d.%d %d %d\n", profile.FileName, block.StartLine, block.StartCol, block.EndLine, block.EndCol, block.NumStmt, block.Count); err != nil {
				return err
			}
		}
	}
	return nil
}

func deepCopyProfile(profile cover.Profile) cover.Profile {
	p := profile
	p.Blocks = make([]cover.ProfileBlock, len(profile.Blocks))
	copy(p.Blocks, profile.Blocks)
	return p
}

// blocksEqual returns true if the blocks refer to the same code, otherwise false.
// It does not care about Count.
func blocksEqual(a cover.ProfileBlock, b cover.ProfileBlock) bool {
	return a.StartCol == b.StartCol && a.StartLine == b.StartLine &&
		a.EndCol == b.EndCol && a.EndLine == b.EndLine && a.NumStmt == b.NumStmt
}

func ensureProfilesMatch(a *cover.Profile, b *cover.Profile) error {
	//fmt.Println("||||  ensure profile match", a.FileName, b.FileName, len(a.Blocks), len(b.Blocks), a.Mode, b.Mode)
	if a.FileName != b.FileName {
		return fmt.Errorf("coverage filename mismatch (%s vs %s)", a.FileName, b.FileName)
	}
	if len(a.Blocks) != len(b.Blocks) {
		return fmt.Errorf("file block count for %s mismatches (%d vs %d)", a.FileName, len(a.Blocks), len(b.Blocks))
	}
	if a.Mode != b.Mode {
		return fmt.Errorf("mode for %s mismatches (%s vs %s)", a.FileName, a.Mode, b.Mode)
	}
	for i, ba := range a.Blocks {
		bb := b.Blocks[i]
		if !blocksEqual(ba, bb) {
			return fmt.Errorf("coverage block mismatch: block #%d for %s (%+v mismatches %+v)", i, a.FileName, ba, bb)
		}
	}
	return nil
}
