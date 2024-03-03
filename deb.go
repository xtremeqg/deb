package deb

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blakesmith/ar"
	"github.com/klauspost/compress/zstd"
	"github.com/pkg/errors"
	"github.com/ulikunitz/xz"
)

type DEBPackage struct {
	Architecture  string
	BuiltUsing    []string
	DebVersion    string
	Depends       []string
	Description   string
	Filename      string
	Homepage      string
	InstalledSize int64
	Maintainer    string
	Modified      time.Time
	Name          string
	Priority      string
	Recommends    []string
	Section       string
	Version       string
}

func Parse(filename string) (*DEBPackage, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot open %s", filename)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "Cannot stat %s", filename)
	}
	deb := &DEBPackage{
		Filename: filename,
		Modified: info.ModTime(),
	}
	ar_reader := ar.NewReader(file)
	for {
		header, err := ar_reader.Next()
		if err != nil {
			if err == io.EOF {
				return deb, nil
			} else {
				return nil, errors.Wrapf(err, "Error reading %s", filename)
			}
		}
		if header.Name == "debian-binary" {
			limit_reader := io.LimitReader(ar_reader, header.Size)
			if err := parse_debian_binary(deb, limit_reader); err != nil {
				return nil, err
			}
		} else if header.Name == "control.tar.gz" {
			limit_reader := io.LimitReader(ar_reader, header.Size)
			if err := parse_control_tar_gz(deb, limit_reader); err != nil {
				return nil, err
			}
		} else if header.Name == "control.tar.xz" {
			limit_reader := io.LimitReader(ar_reader, header.Size)
			if err := parse_control_tar_xz(deb, limit_reader); err != nil {
				return nil, err
			}
		} else if header.Name == "control.tar.zst" {
			limit_reader := io.LimitReader(ar_reader, header.Size)
			if err := parse_control_tar_zst(deb, limit_reader); err != nil {
				return nil, err
			}
		} else if header.Name == "data.tar.gz" {
			return deb, nil
		} else if header.Name == "data.tar.xz" {
			return deb, nil
		} else if header.Name == "data.tar.zst" {
			return deb, nil
		} else {
			fmt.Println(header.Name)
		}
	}
}

func parse_debian_binary(deb *DEBPackage, reader io.Reader) error {
	version, err := bufio.NewReader(reader).ReadString(0)
	if err != io.EOF {
		return errors.Wrap(err, "Cannot read DEB version string")
	}
	deb.DebVersion = strings.TrimSpace(version)
	return nil
}

func parse_control_tar_gz(deb *DEBPackage, reader io.Reader) error {
	gz_reader, err := gzip.NewReader(reader)
	if err != nil {
		return errors.Wrap(err, "Error decompressing control.tar.gz")
	}
	defer gz_reader.Close()
	return parse_control_tar(deb, gz_reader)
}

func parse_control_tar_xz(deb *DEBPackage, reader io.Reader) error {
	xz_reader, err := xz.NewReader(reader)
	if err != nil {
		return errors.Wrap(err, "Error decompressing control.tar.xz")
	}
	return parse_control_tar(deb, xz_reader)
}

func parse_control_tar_zst(deb *DEBPackage, reader io.Reader) error {
	zst_reader, err := zstd.NewReader(reader)
	if err != nil {
		return errors.Wrap(err, "error decompressing control.tar.zst")
	}
	defer zst_reader.Close()
	return parse_control_tar(deb, zst_reader)
}

func parse_control_tar(deb *DEBPackage, reader io.Reader) error {
	tar_reader := tar.NewReader(reader)
	for {
		header, err := tar_reader.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			} else {
				return errors.Wrap(err, "Error reading control.tar")
			}
		}
		if header.Name == "./control" {
			limit_reader := io.LimitReader(tar_reader, header.Size)
			parse_control(deb, limit_reader)
		}
	}
}

func parse_control(deb *DEBPackage, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	in_description := false
	for scanner.Scan() {
		line := scanner.Text()
		if in_description {
			if !strings.ContainsAny(line[0:1], " \t\r\n") {
				in_description = false
			} else {
				deb.Description += line
			}
		}
		if !in_description {
			name, value := parse_control_field(deb, line)
			switch name {
			case "Architecture":
				deb.Architecture = value
			case "Built-Using":
				deb.BuiltUsing = strings.SplitN(value, ", ", -1)
			case "Depends":
				deb.Depends = strings.SplitN(value, ", ", -1)
			case "Description":
				deb.Description = value
				in_description = true
			case "Homepage":
				deb.Homepage = value
			case "Installed-Size":
				if size, err := strconv.ParseInt(value, 10, 64); err != nil {
					return errors.Wrap(err, "Error parsing Installed-Size")
				} else {
					deb.InstalledSize = size
				}
			case "Maintainer":
				deb.Maintainer = value
			case "Package":
				deb.Name = value
			case "Priority":
				deb.Priority = value
			case "Recommends":
				deb.Recommends = strings.SplitN(value, ", ", -1)
			case "Section":
				deb.Section = value
			case "Version":
				deb.Version = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.Wrap(err, "Error parsing control file")
	}
	return nil
}

func parse_control_field(deb *DEBPackage, line string) (string, string) {
	if fields := strings.SplitN(line, ": ", 2); len(fields) == 2 {
		return strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
	}
	return "", ""
}
