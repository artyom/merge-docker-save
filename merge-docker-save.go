// Command merge-docker-save repacks output of docker save command called for
// single image to a tar stream with merged content of all image layers
//
// Usage:
//
// 	docker save image:tag | merge-docker-save > image-fs.tar
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/artyom/autoflags"
)

func main() {
	args := struct {
		File string `flag:"o,file to write output to instead of stdout"`
		Gzip bool   `flag:"gzip,compress output with gzip"`
	}{}
	autoflags.Parse(&args)
	if err := do(args.File, args.Gzip, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func do(name string, gzip bool, input io.Reader) error {
	output, err := openOutput(name, gzip)
	if err != nil {
		return err
	}
	defer output.Close()
	if err := repack(output, input); err != nil {
		return err
	}
	return output.Close()
}

func repack(out io.Writer, input io.Reader) error {
	tr := tar.NewReader(input)
	tw := tar.NewWriter(out)
	layers := make(map[string]*os.File)
	var mlayers []*layerMeta
	defer func() {
		for _, f := range layers {
			f.Close()
		}
	}()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			if err := fillSkips(layers, mlayers); err != nil {
				return err
			}
			for _, meta := range mlayers {
				f, ok := layers[meta.name]
				if !ok {
					return fmt.Errorf("manifest references unknown layer %q", meta.name)
				}
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return err
				}
				if err := copyStream(tw, tar.NewReader(f), meta.skip); err != nil {
					return err
				}
			}
			return tw.Close()
		}
		if err != nil {
			return err
		}
		if strings.HasSuffix(hdr.Name, "/layer.tar") {
			f, err := dumpStream(tr)
			if err != nil {
				return err
			}
			layers[hdr.Name] = f
			continue
		}
		if hdr.Name == "manifest.json" {
			if mlayers, err = decodeLayerList(tr); err != nil {
				return err
			}
		}
	}
}

func copyStream(tw *tar.Writer, tr *tar.Reader, skip map[string]struct{}) error {
tarLoop:
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if _, ok := skip[hdr.Name]; ok {
			continue
		}
		if strings.HasPrefix(path.Base(hdr.Name), tombstone) {
			continue
		}
		for prefix := range skip {
			if strings.HasPrefix(hdr.Name, prefix+"/") {
				continue tarLoop
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}

func decodeLayerList(r io.Reader) ([]*layerMeta, error) {
	data := []struct {
		Layers []string
	}{}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return nil, err
	}
	if l := len(data); l != 1 {
		return nil, fmt.Errorf("manifest.json describes %d objects, call docker save for a single image", l)
	}
	out := make([]*layerMeta, len(data[0].Layers))
	for i, name := range data[0].Layers {
		out[i] = &layerMeta{name: name}
	}
	return out, nil
}

func dumpStream(r io.Reader) (*os.File, error) {
	f, err := ioutil.TempFile("", "merge-docker-save-")
	if err != nil {
		return nil, err
	}
	os.Remove(f.Name())
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func openOutput(name string, compress bool) (io.WriteCloser, error) {
	var wc io.WriteCloser = os.Stdout
	if name != "" {
		f, err := os.Create(name)
		if err != nil {
			return nil, err
		}
		wc = f
	}
	if !compress {
		return wc, nil
	}
	return &writerChain{gzip.NewWriter(wc), wc}, nil
}

type writerChain []io.WriteCloser

// Write implements io.Writer by writing to the first Writer in writerChain
func (w writerChain) Write(b []byte) (int, error) { return w[0].Write(b) }

// Close implements io.Closer by closing every Closer in a writerChain and
// returning the first captured non-nil error it encountered.
func (w writerChain) Close() error {
	var err error
	for _, c := range w {
		if err2 := c.Close(); err2 != nil && err == nil {
			err = err2
		}
	}
	return err
}

type layerMeta struct {
	name string
	skip map[string]struct{}
}

// fillSkips fills skip fields of mlayers elements from the tombstone items
// discovered in files referenced in layers map. skip fields filled in such
// a way that for each layer it holds a set of names that should be skipped when
// repacking tar stream since these items would be removed by the following
// layers.
func fillSkips(layers map[string]*os.File, mlayers []*layerMeta) error {
	for i := len(mlayers) - 1; i > 0; i-- {
		meta := mlayers[i]
		f, ok := layers[meta.name]
		if !ok {
			return fmt.Errorf("manifest references unknown layer %q", meta.name)
		}
		skips, err := findSkips(f)
		if err != nil {
			return err
		}
		if skips == nil {
			continue
		}
		for _, meta := range mlayers[:i] {
			if meta.skip == nil {
				meta.skip = make(map[string]struct{})
			}
			for _, s := range skips {
				meta.skip[s] = struct{}{}
			}
		}
	}
	return nil
}

// findSkips scans tar archive for tombstone items and returns list of
// corresponding file names.
func findSkips(f io.ReadSeeker) ([]string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var skips []string
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return skips, nil
		}
		if err != nil {
			return nil, err
		}
		if base := path.Base(hdr.Name); strings.HasPrefix(base, tombstone) && base != tombstone {
			skips = append(skips, path.Join(path.Dir(hdr.Name), strings.TrimPrefix(base, tombstone)))
		} else {
			// workaround for GNU tar bug: https://gist.github.com/artyom/926ec9c49a2077f2820053274f0b1b16
			skips = append(skips, hdr.Name)
		}
	}
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: docker save image:tag | %s > image-fs.tar\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}

const tombstone = ".wh." // prefix docker uses to mark deleted files
