// Command merge-docker-save repacks output of docker save command called for
// single image to a tar stream with merged content of all image layers
package main

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
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
	var layers []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return tw.Close()
		}
		if err != nil {
			return err
		}
		if strings.HasSuffix(hdr.Name, layerSuffix) {
			layers = append(layers, strings.TrimSuffix(hdr.Name, layerSuffix))
			if err := copyStream(tw, tar.NewReader(tr)); err != nil {
				return err
			}
			continue
		}
		if hdr.Name == "manifest.json" {
			mlayers, err := decodeLayerList(tr)
			if err != nil {
				return err
			}
			if err := compareLayers(layers, mlayers); err != nil {
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
		return nil, fmt.Errorf("manifest has %d objects, want 1", l)
	}
	out := make([]string, len(data[0].Layers))
	for i, s := range data[0].Layers {
		out[i] = strings.TrimSuffix(s, layerSuffix)
	}
	return out, nil
}

func compareLayers(layers, mlayers []string) error {
	err := fmt.Errorf("unpacked layers and listed in manifest.json differ")
	if len(layers) != len(mlayers) {
		return err
	}
	for i := range layers {
		if layers[i] != mlayers[i] {
			return err
		}
	}
	return nil
}

func openOutput(name string) (io.WriteCloser, error) {
	if name == "" {
		return os.Stdout, nil
	}
	return os.Create(name)
}

const layerSuffix = "/layer.tar"