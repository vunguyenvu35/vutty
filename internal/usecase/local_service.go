package usecase

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vutty/internal/domain"
)

// LocalService browses and edits this computer's files for the SFTP screen (the "Local" pane). Paths
// must be absolute; the vault, the database and the app's own files get no special treatment because
// the user owns them, but roots and the home folder can never be deleted or renamed away.
type LocalService struct{}

func NewLocalService() *LocalService { return &LocalService{} }

var errLocalPath = errors.New("enter an absolute path")

func localPath(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return "", errLocalPath
	}
	return filepath.Clean(p), nil
}

// Home returns the user's home folder (the pane's starting point).
func (s *LocalService) Home() (string, error) { return os.UserHomeDir() }

// List returns the entries of a folder, folders first, then by name. Hidden files are included: the
// UI decides whether to show them.
func (s *LocalService) List(dir string) ([]domain.RemoteEntry, error) {
	d, err := localPath(dir)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	out := make([]domain.RemoteEntry, 0, len(items))
	for _, it := range items {
		info, err := it.Info()
		if err != nil {
			continue // vanished while listing
		}
		e := domain.RemoteEntry{Name: it.Name(), Path: filepath.Join(d, it.Name()), Size: info.Size(), Mode: info.Mode().String(), ModTime: info.ModTime(), IsDir: info.IsDir()}
		if info.Mode()&os.ModeSymlink != 0 {
			e.IsLink = true
			if t, err := os.Stat(e.Path); err == nil {
				e.IsDir = t.IsDir()
				e.Size = t.Size()
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Mkdir creates one folder; it never overwrites.
func (s *LocalService) Mkdir(path string) error {
	p, err := localPath(path)
	if err != nil {
		return err
	}
	return os.Mkdir(p, 0o755)
}

// Rename moves a file or folder inside the same filesystem and refuses to replace an existing one.
func (s *LocalService) Rename(from, to string) error {
	a, err := localPath(from)
	if err != nil {
		return err
	}
	b, err := localPath(to)
	if err != nil {
		return err
	}
	if err := s.protected(a); err != nil {
		return err
	}
	if _, err := os.Lstat(b); err == nil {
		return fmt.Errorf("%s already exists", b)
	}
	return os.Rename(a, b)
}

// Chmod sets the permission bits of a file or folder on this computer.
func (s *LocalService) Chmod(path string, mode int) error {
	p, err := localPath(path)
	if err != nil {
		return err
	}
	if err := s.protected(p); err != nil {
		return err
	}
	if mode < 0 || mode > 0o7777 {
		return errors.New("permissions out of range")
	}
	return os.Chmod(p, os.FileMode(mode))
}

// TempDir makes a new private folder for files that are only looked at (a remote file opened in an app).
func (s *LocalService) TempDir() (string, error) { return os.MkdirTemp("", "vutty-open-") }

// Delete removes a file, or a folder with everything in it when recursive is true.
func (s *LocalService) Delete(path string, recursive bool) error {
	p, err := localPath(path)
	if err != nil {
		return err
	}
	if err := s.protected(p); err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

// protected refuses the filesystem root and the home folder.
func (s *LocalService) protected(p string) error {
	if p == filepath.Dir(p) {
		return errors.New("refusing to change the root folder")
	}
	if h, err := os.UserHomeDir(); err == nil && filepath.Clean(h) == p {
		return errors.New("refusing to change the home folder")
	}
	return nil
}

// copyItem is one file or folder of a local copy: where it comes from and where it goes.
type copyItem struct {
	src, dst string
	dir      bool
	mode     os.FileMode
	size     int64
}

// planCopy lists everything that copying sources into destDir would create, folders before what is in them. Links and
// special files are not followed: they are returned as ignored.
func (s *LocalService) planCopy(sources []string, destDir string) (items []copyItem, ignored []string, dest string, err error) {
	dest, err = localPath(destDir)
	if err != nil {
		return nil, nil, "", err
	}
	if st, e := os.Stat(dest); e != nil || !st.IsDir() {
		return nil, nil, "", fmt.Errorf("%s is not a folder", dest)
	}
	for _, raw := range sources {
		src, e := localPath(raw)
		if e != nil {
			return nil, nil, "", e
		}
		info, e := os.Lstat(src)
		if e != nil {
			return nil, nil, "", e
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			ignored = append(ignored, src)
			continue
		}
		top := filepath.Join(dest, filepath.Base(src))
		if info.IsDir() && (dest == src || strings.HasPrefix(dest+string(filepath.Separator), src+string(filepath.Separator))) {
			return nil, nil, "", fmt.Errorf("cannot copy %s into itself", src)
		}
		e = filepath.WalkDir(src, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			fi, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			rel, _ := filepath.Rel(src, p)
			dst := filepath.Join(top, rel)
			switch {
			case fi.IsDir():
				items = append(items, copyItem{src: p, dst: dst, dir: true, mode: fi.Mode().Perm()})
			case fi.Mode().IsRegular():
				items = append(items, copyItem{src: p, dst: dst, mode: fi.Mode().Perm(), size: fi.Size()})
			default:
				ignored = append(ignored, p)
			}
			return nil
		})
		if e != nil {
			return nil, nil, "", e
		}
	}
	return items, ignored, dest, nil
}

// Conflicts returns the files that copying sources into destDir would replace (so the UI can ask once).
func (s *LocalService) Conflicts(sources []string, destDir string) ([]string, error) {
	items, _, _, err := s.planCopy(sources, destDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, it := range items {
		if it.dir {
			continue
		}
		if st, e := os.Lstat(it.dst); e == nil {
			if st.IsDir() {
				return nil, fmt.Errorf("%s is a folder, not a file", it.dst)
			}
			out = append(out, it.dst)
		}
	}
	return out, nil
}

// Copy copies files and folders (with everything inside) into destDir on this computer. A file that already exists is
// left as it is unless overwrite is true; a file is never copied onto itself. Links and special files are ignored.
func (s *LocalService) Copy(sources []string, destDir string, overwrite bool) (TransferResult, error) {
	res := TransferResult{Skipped: []string{}, Ignored: []string{}}
	items, ignored, _, err := s.planCopy(sources, destDir)
	if err != nil {
		return res, err
	}
	res.Ignored = append(res.Ignored, ignored...)
	for _, it := range items {
		if it.dir {
			if st, e := os.Lstat(it.dst); e == nil {
				if !st.IsDir() {
					return res, fmt.Errorf("%s exists and is not a folder", it.dst)
				}
				continue
			}
			if err := os.Mkdir(it.dst, it.mode|0o700); err != nil {
				return res, err
			}
			res.Dirs++
			continue
		}
		if it.src == it.dst {
			res.Skipped = append(res.Skipped, it.dst)
			continue
		}
		if _, e := os.Lstat(it.dst); e == nil && !overwrite {
			res.Skipped = append(res.Skipped, it.dst)
			continue
		}
		if err := copyFile(it.src, it.dst, it.mode); err != nil {
			return res, err
		}
		res.Files++
		res.Bytes += it.size
	}
	return res, nil
}

// copyFile writes src to a temporary file next to dst and renames it over dst, so a failure never leaves half a file.
func copyFile(src, dst string, mode os.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".vutty-copy-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, in); err != nil {
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
