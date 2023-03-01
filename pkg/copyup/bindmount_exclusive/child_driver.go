package bindmount_exclusive

import (
	"errors"
	"fmt"
	"github.com/rootless-containers/rootlesskit/pkg/copyup"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
)

func NewChildDriver(ignorePaths []string) copyup.ChildDriver {
	return &childDriver{ignorePaths}
}

type childDriver struct {
	IgnoreList []string
}

func (d *childDriver) isIgnoredTarget(path string) bool {
	for _, x := range d.IgnoreList {
		//fmt.Printf("path: %s x: %s\n", path, x)
		if path == x {
			return true
		}
	}
	return false
}

func (d *childDriver) CopyUp(dirs []string) ([]string, error) {
	// we create bind0 outside of StateDir so as to allow
	// copying up /run with stateDir=/run/user/1001/rootlesskit/default.
	bind0, err := os.MkdirTemp("/tmp", "rootlesskit-b")
	if err != nil {
		return nil, fmt.Errorf("creating bind0 directory under /tmp: %w", err)
	}
	defer os.RemoveAll(bind0)
	var copied []string
	for _, dir := range dirs {
		dir := filepath.Clean(dir)
		if dir == "/tmp" {
			// TODO: we can support copy-up /tmp by changing bind0TempDir
			return copied, errors.New("/tmp cannot be copied up")
		}

		if err := unix.Mount(dir, bind0, "", uintptr(unix.MS_BIND|unix.MS_REC), ""); err != nil {
			return copied, fmt.Errorf("failed to create bind mount on %s: %w", dir, err)
		}

		if err := unix.Mount("none", dir, "tmpfs", 0, ""); err != nil {
			return copied, fmt.Errorf("failed to mount tmpfs on %s: %w", dir, err)
		}

		bind1, err := os.MkdirTemp(dir, ".ro")
		if err != nil {
			return copied, fmt.Errorf("creating a directory under %s: %w", dir, err)
		}

		sources, err := os.ReadDir(bind0)
		if err != nil {
			return copied, fmt.Errorf("failed to read dir %s: %w", dir, err)
		}
		for _, s := range sources {
			if d.isIgnoredTarget(filepath.Join(dir, s.Name())) {
				continue
			}
			copyTarget := filepath.Join(bind1, s.Name())
			if s.IsDir() {
				if err := os.Mkdir(copyTarget, 755); err != nil {
					return copied, fmt.Errorf("failed to create dir %s: %w", copyTarget, err)
				}
				mountSrc := filepath.Join(bind0, s.Name())
				if err := unix.Mount(copyTarget, mountSrc, "", uintptr(unix.MS_BIND|unix.MS_REC), ""); err != nil {
					return copied, fmt.Errorf("failed to mount %s to %s: %w", mountSrc, copyTarget, err)
				}
			} else if s.Type().IsRegular() {
				ifd, _ := os.Open(filepath.Join(bind0, s.Name()))
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "failed to open file %s: %w\n", s.Name(), err)
					continue
				}
				ofd, err := os.Create(copyTarget)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "failed to create file %s: %w\n", copyTarget, err)
					continue
				}
				if _, err = io.Copy(ofd, ifd); err != nil {
					//_, _ = fmt.Fprintf(os.Stderr, "failed to copy file %s %s: %s\n", s, copyTarget, err.Error())
				}
			}
		}

		files, err := os.ReadDir(bind1)
		if err != nil {
			return copied, fmt.Errorf("reading dir %s: %w", bind1, err)
		}
		for _, f := range files {
			fFull := filepath.Join(bind1, f.Name())
			var symlinkSrc string
			if f.Type()&os.ModeSymlink != 0 {
				symlinkSrc, err = os.Readlink(fFull)
				if err != nil {
					return copied, fmt.Errorf("reading dir %s: %w", fFull, err)
				}
			} else {
				symlinkSrc = filepath.Join(filepath.Base(bind1), f.Name())
			}
			symlinkDst := filepath.Join(dir, f.Name())
			if err = os.RemoveAll(symlinkDst); err != nil {
				return copied, fmt.Errorf("removing %s: %w", symlinkDst, err)
			}
			if err := os.Symlink(symlinkSrc, symlinkDst); err != nil {
				return copied, fmt.Errorf("symlinking %s to %s: %w", symlinkSrc, symlinkDst, err)
			}
		}
		copied = append(copied, dir)
	}
	return copied, nil
}
