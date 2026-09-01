package skill

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// TreeHash computes the git tree object id of a directory, the value the
// pnpx skills lock file records for skills installed from GitHub.
func TreeHash(dir string) (string, error) {
	sum, err := treeSum(dir)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

func treeSum(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type entry struct {
		mode, name string
		sum        []byte
		sortKey    string
	}
	var list []entry
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		var ent entry
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return nil, err
			}
			ent = entry{mode: "120000", name: e.Name(), sum: blobSum([]byte(target)), sortKey: e.Name()}
		case info.IsDir():
			if e.Name() == ".git" || e.Name() == "node_modules" {
				continue
			}
			sum, err := treeSum(p)
			if err != nil {
				return nil, err
			}
			// git sorts directories as if their name ended in a slash
			ent = entry{mode: "40000", name: e.Name(), sum: sum, sortKey: e.Name() + "/"}
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			mode := "100644"
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
			ent = entry{mode: mode, name: e.Name(), sum: blobSum(b), sortKey: e.Name()}
		default:
			continue
		}
		list = append(list, ent)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sortKey < list[j].sortKey })
	var body []byte
	for _, e := range list {
		body = append(body, e.mode...)
		body = append(body, ' ')
		body = append(body, e.name...)
		body = append(body, 0)
		body = append(body, e.sum...)
	}
	return objectSum("tree", body), nil
}

func blobSum(content []byte) []byte { return objectSum("blob", content) }

func objectSum(kind string, body []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(body))
	h.Write(body)
	return h.Sum(nil)
}
