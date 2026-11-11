package docsafe

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// xattrs maps attribute names to their values.
type xattrs map[string][]byte

// kernelManaged names the attributes the system sets and guards itself, so a
// program cannot copy them. Measured on macOS 27 (APFS): the kernel gives every
// newly created file com.apple.provenance (the creating app's identity);
// fsetxattr and fremovexattr on it both return success and change nothing.
// Copying it is therefore impossible, and verifying it would be meaningless.
// The replaced file ends up with Pisar's provenance, which is also true: Pisar
// wrote it. Nothing else is exempt: any other failure aborts the save.
var kernelManaged = map[string]bool{
	"com.apple.provenance": true,
}

// xattrRetries bounds the size-then-read loops below; an attribute that keeps
// growing between the two calls is not worth chasing.
const xattrRetries = 5

// listXattrs returns the names of all attributes of f.
func listXattrs(f *os.File) ([]string, error) {
	fd := int(f.Fd())
	for range xattrRetries {
		size, err := unix.Flistxattr(fd, nil)
		if err != nil {
			return nil, noXattrIsEmpty(err)
		}
		if size == 0 {
			return nil, nil
		}
		buf := make([]byte, size)
		n, err := unix.Flistxattr(fd, buf)
		if errors.Is(err, unix.ERANGE) {
			continue // the list grew between the two calls
		}
		if err != nil {
			return nil, noXattrIsEmpty(err)
		}
		return strings.Split(strings.TrimSuffix(string(buf[:n]), "\x00"), "\x00"), nil
	}
	return nil, errors.New("the attribute list kept changing")
}

// noXattrIsEmpty turns "this volume has no extended attributes" into an empty
// list (nil error) and leaves every other error alone. On such a volume both
// the original and its replacement have none, so nothing is lost.
func noXattrIsEmpty(err error) error {
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	return err
}

func getXattr(f *os.File, name string) ([]byte, error) {
	fd := int(f.Fd())
	for range xattrRetries {
		size, err := unix.Fgetxattr(fd, name, nil)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size)
		n, err := unix.Fgetxattr(fd, name, buf)
		if errors.Is(err, unix.ERANGE) {
			continue // the value grew between the two calls
		}
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
	return nil, errors.New("the attribute kept changing")
}

// readXattrs reads every attribute of f except the kernel-managed ones.
func readXattrs(f *os.File) (xattrs, error) {
	names, err := listXattrs(f)
	if err != nil {
		return nil, fmt.Errorf("list extended attributes: %w", err)
	}
	out := make(xattrs, len(names))
	for _, n := range names {
		if kernelManaged[n] {
			continue
		}
		v, err := getXattr(f, n)
		if err != nil {
			return nil, fmt.Errorf("read extended attribute %q: %w", n, err)
		}
		out[n] = v
	}
	return out, nil
}

// copyXattrs makes the attributes of dst exactly want (the original's). Every
// attribute is set, then dst is read back: a missing or different one, or an
// extra one that the system put there, is fixed or reported. Nothing is skipped
// except kernelManaged.
func copyXattrs(dst *os.File, want xattrs) error {
	fd := int(dst.Fd())
	// Sorted so that a failure is reproducible.
	for _, n := range slices.Sorted(maps.Keys(want)) {
		if err := unix.Fsetxattr(fd, n, want[n], 0); err != nil {
			return fmt.Errorf("set extended attribute %q: %w", n, err)
		}
	}
	got, err := readXattrs(dst)
	if err != nil {
		return err
	}
	for n := range got {
		if _, ok := want[n]; !ok {
			if err := unix.Fremovexattr(fd, n); err != nil {
				return fmt.Errorf("remove extended attribute %q that the original does not have: %w", n, err)
			}
		}
	}
	for n, v := range want {
		if g, ok := got[n]; !ok || !bytes.Equal(g, v) {
			return fmt.Errorf("extended attribute %q was not transferred intact", n)
		}
	}
	return nil
}
