package data

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// data.compression's n8n-style operations: "compress" packs one or more
// binary properties into a zip / tar / tar.gz archive (or gzips each one),
// "decompress" unpacks zip, tar, tar.gz and gzip files into one binary
// property per extracted file. The older gzip/gunzip/zip/unzip operations in
// compression.go are unchanged.

// maxArchiveEntries bounds the files one archive may unpack to; together
// with maxBinaryBytes (total decompressed size) it is the zip-bomb guard.
const maxArchiveEntries = 10000

// alreadyCompressed are extensions zip stores rather than deflates.
var alreadyCompressed = map[string]bool{
	"7z": true, "aac": true, "avi": true, "bz2": true, "flac": true, "gif": true, "gz": true,
	"jpeg": true, "jpg": true, "m4a": true, "mkv": true, "mov": true, "mp3": true, "mp4": true,
	"ogg": true, "png": true, "rar": true, "tgz": true, "webm": true, "webp": true, "xz": true,
	"zip": true, "docx": true, "xlsx": true, "pptx": true, "odt": true, "ods": true,
}

type namedFile struct {
	prop string
	data []byte
	meta binaryMeta
}

func (n *CompressionNode) executeFiles(input workflow.NodeInput, config map[string]interface{}, op string) ([]workflow.NodeOutput, error) {
	props := cfgNames(config, "binary_property_name")
	if len(props) == 0 {
		props = []string{defaultBinaryProperty}
	}
	out := make([]workflow.Item, 0, len(input.Items))
	for i, item := range input.Items {
		var in []namedFile
		for _, p := range props {
			data, meta, err := readBinary(item, p)
			if err != nil {
				return nil, fmt.Errorf("data.compression: item %d: binary_property_name: %w", i, err)
			}
			in = append(in, namedFile{prop: p, data: data, meta: completeMeta(meta, data)})
		}
		var (
			files []namedFile
			err   error
		)
		if op == "compress" {
			files, err = compressFiles(in, config)
		} else {
			files, err = decompressFiles(in, cfgString(config, "output_prefix", "file_"))
		}
		if err != nil {
			return nil, fmt.Errorf("data.compression: item %d: %w", i, err)
		}
		res := cloneItem(item)
		for _, p := range append(binaryProperties(item), props...) {
			dropBinary(&res, p)
		}
		names := make([]string, len(files))
		datas := make([][]byte, len(files))
		metas := make([]binaryMeta, len(files))
		for j, f := range files {
			names[j], datas[j], metas[j] = f.prop, f.data, f.meta
		}
		if len(files) > 0 {
			putBinaries(res.JSON, names, datas, metas)
		}
		out = append(out, res)
	}
	return mainOutput(out), nil
}

func compressFiles(in []namedFile, config map[string]interface{}) ([]namedFile, error) {
	format := cfgString(config, "output_format", "zip")
	outProp := cfgString(config, "binary_property_output", defaultBinaryProperty)
	fileName := cfgString(config, "file_name", "")
	switch format {
	case "zip", "tar", "targz":
		ext := map[string]string{"zip": "zip", "tar": "tar", "targz": "tar.gz"}[format]
		if fileName == "" {
			fileName = "archive." + ext
		}
		var (
			data []byte
			err  error
		)
		if format == "zip" {
			data, err = buildZip(in)
		} else {
			data, err = buildTar(in, format == "targz")
		}
		if err != nil {
			return nil, fmt.Errorf("output_format %s: %w", format, err)
		}
		mime := map[string]string{"zip": "application/zip", "tar": "application/x-tar", "targz": "application/gzip"}[format]
		return []namedFile{{prop: outProp, data: data, meta: binaryMeta{FileName: fileName, MimeType: mime, Extension: strings.TrimPrefix(path.Ext(fileName), ".")}}}, nil
	case "gzip":
		var out []namedFile
		for i, f := range in {
			base := strings.TrimSuffix(strings.TrimSuffix(fileName, ".gz"), ".gzip")
			if base == "" {
				base = strings.SplitN(f.meta.FileName, ".", 2)[0]
			}
			if base == "" {
				base = f.prop
			}
			name := base
			if f.meta.Extension != "" {
				name += "." + strings.ToLower(f.meta.Extension)
			}
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			zw.Name = name
			if _, err := zw.Write(f.data); err != nil {
				return nil, err
			}
			if err := zw.Close(); err != nil {
				return nil, err
			}
			prop := outProp
			if i > 0 {
				prop += strconv.Itoa(i)
			}
			out = append(out, namedFile{prop: prop, data: buf.Bytes(), meta: binaryMeta{FileName: name + ".gz", MimeType: "application/gzip", Extension: "gz"}})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("output_format: %q is not one of zip, gzip, tar, targz", format)
	}
}

// entryNames gives each input file a unique name inside an archive.
func entryNames(in []namedFile) []string {
	used := map[string]int{}
	names := make([]string, len(in))
	for i, f := range in {
		name := f.meta.FileName
		if name == "" {
			name = f.prop
			if f.meta.Extension != "" {
				name += "." + f.meta.Extension
			}
		}
		if n := used[name]; n > 0 {
			ext := path.Ext(name)
			used[name]++
			name = strings.TrimSuffix(name, ext) + "_" + strconv.Itoa(n+1) + ext
		} else {
			used[name] = 1
		}
		names[i] = name
	}
	return names
}

func buildZip(in []namedFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, name := range entryNames(in) {
		method := zip.Deflate
		if alreadyCompressed[strings.ToLower(in[i].meta.Extension)] {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(in[i].data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildTar(in []namedFile, gz bool) ([]byte, error) {
	var buf bytes.Buffer
	var w io.Writer = &buf
	var zw *gzip.Writer
	if gz {
		zw = gzip.NewWriter(&buf)
		w = zw
	}
	tw := tar.NewWriter(w)
	for i, name := range entryNames(in) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(in[i].data)), ModTime: time.Now(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(in[i].data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
