package data

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Unpacking for data.compression's "decompress" operation. Every byte
// inflated counts against one budget of maxBinaryBytes per item, and an
// archive may hold at most maxArchiveEntries files.

type unpackBudget struct{ left int64 }

// read inflates r, failing once the item's total would pass the budget.
func (b *unpackBudget) read(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, b.left+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if int64(len(data)) > b.left {
		return nil, fmt.Errorf("%s: decompressed data exceeds the %d byte limit", name, maxBinaryBytes)
	}
	b.left -= int64(len(data))
	return data, nil
}

// archiveKind decides how to unpack a file: by its name / extension first,
// then by its leading bytes.
func archiveKind(f namedFile) string {
	name := strings.ToLower(f.meta.FileName)
	ext := strings.ToLower(f.meta.Extension)
	switch {
	case strings.HasSuffix(name, ".tar.gz") || ext == "tgz":
		return "targz"
	case ext == "tar":
		return "tar"
	case ext == "zip":
		return "zip"
	case ext == "gz" || ext == "gzip":
		return "gzip"
	}
	d := f.data
	switch {
	case bytes.HasPrefix(d, []byte("PK\x03\x04")) || bytes.HasPrefix(d, []byte("PK\x05\x06")):
		return "zip"
	case len(d) >= 2 && d[0] == 0x1f && d[1] == 0x8b:
		return "gzip"
	case len(d) >= 262 && string(d[257:262]) == "ustar":
		return "tar"
	}
	return ""
}

func decompressFiles(in []namedFile, prefix string) ([]namedFile, error) {
	budget := &unpackBudget{left: maxBinaryBytes}
	var out []namedFile
	next := 0
	add := func(name string, data []byte) {
		out = append(out, namedFile{prop: prefix + strconv.Itoa(next), data: data, meta: binaryMeta{FileName: path.Base(name)}})
		next++
	}
	for _, f := range in {
		switch archiveKind(f) {
		case "zip":
			if err := unzipAll(f.data, budget, add); err != nil {
				return nil, fmt.Errorf("binary property %q: %w", f.prop, err)
			}
		case "tar":
			if err := untarAll(bytes.NewReader(f.data), budget, add); err != nil {
				return nil, fmt.Errorf("binary property %q: %w", f.prop, err)
			}
		case "targz":
			zr, err := gzip.NewReader(bytes.NewReader(f.data))
			if err != nil {
				return nil, fmt.Errorf("binary property %q: %w", f.prop, err)
			}
			if err := untarAll(zr, budget, add); err != nil {
				return nil, fmt.Errorf("binary property %q: %w", f.prop, err)
			}
		case "gzip":
			zr, err := gzip.NewReader(bytes.NewReader(f.data))
			if err != nil {
				return nil, fmt.Errorf("binary property %q: %w", f.prop, err)
			}
			data, err := budget.read(zr, f.prop)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSuffix(strings.TrimSuffix(f.meta.FileName, ".gz"), ".gzip")
			if name == "" || name == f.meta.FileName {
				name = zr.Name
			}
			if name == "" {
				m := completeMeta(binaryMeta{}, data)
				name = "file"
				if m.Extension != "" {
					name += "." + m.Extension
				}
			}
			add(name, data)
		default:
			return nil, fmt.Errorf("binary property %q: %q is not a zip, gzip, tar or tar.gz file", f.prop, f.meta.FileName)
		}
	}
	return out, nil
}

func unzipAll(data []byte, budget *unpackBudget, add func(string, []byte)) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive has %d entries, limit is %d", len(zr.File), maxArchiveEntries)
	}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || strings.Contains(zf.Name, "__MACOSX") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("%s: %w", zf.Name, err)
		}
		b, err := budget.read(rc, zf.Name)
		rc.Close()
		if err != nil {
			return err
		}
		add(zf.Name, b)
	}
	return nil
}

func untarAll(r io.Reader, budget *unpackBudget, add func(string, []byte)) error {
	tr := tar.NewReader(r)
	for n := 0; ; n++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if n >= maxArchiveEntries {
			return fmt.Errorf("archive has more than %d entries", maxArchiveEntries)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.Contains(hdr.Name, "__MACOSX") || strings.HasPrefix(path.Base(hdr.Name), "._") {
			continue
		}
		b, err := budget.read(tr, hdr.Name)
		if err != nil {
			return err
		}
		add(hdr.Name, b)
	}
}
