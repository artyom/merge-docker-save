// Command merge-docker-save repacks output of docker save command called for
// single image to a tar stream with merged content of all image layers
//
// Usage:
//
// 	docker save image:tag | merge-docker-save > image-fs.tar
package main

import (
	"archive/tar"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/artyom/autoflags"
)

func main() {
	args := struct {
		File string `flag:"o,file to write output to instead of stdout"`
	}{}
	autoflags.Parse(&args)
	if err := do(args.File, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func do(name string, input io.Reader) error {
	output, err := openOutput(name)
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
	layers := make(map[string]io.ReadCloser)
	var mlayers []string
	defer func() {
		for _, f := range layers {
			f.Close()
		}
	}()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			for _, name := range mlayers {
				f, ok := layers[name]
				if !ok {
					return fmt.Errorf("manifest references unknown layer %q", name)
				}
				if err := copyStream(tw, tar.NewReader(f)); err != nil {
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
			var err error
			mlayers, err = decodeLayerList(tr)
			if err != nil {
				return err
			}
		}
		if _, err := io.Copy(ioutil.Discard, tr); err != nil {
			return err
		}
	}
}

func copyStream(tw *tar.Writer, tr *tar.Reader) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}

func decodeLayerList(r io.Reader) ([]string, error) {
	data := []struct {
		Layers []string
	}{}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return nil, err
	}
	if l := len(data); l != 1 {
		return nil, fmt.Errorf("manifest.json describes %d objects, call docker save for a single image", l)
	}
	return data[0].Layers, nil
}

func dumpStream(r io.Reader) (io.ReadCloser, error) {
	f, err := ioutil.TempFile("", "merge-docker-save-")
	if err != nil {
		return nil, err
	}
	os.Remove(f.Name())
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func openOutput(name string) (io.WriteCloser, error) {
	if name == "" {
		return os.Stdout, nil
	}
	return os.Create(name)
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: docker save image:tag | %s > image-fs.tar\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}
