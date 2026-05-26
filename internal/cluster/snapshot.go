package cluster

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/raft"
)

const (
	clusterDirName        = "_cluster"
	snapshotMetadataName  = ".cluster_snapshot.json"
	snapshotStagingPrefix = "snapshot-stage-"
)

type snapshotMetadata struct {
	Applied appliedState `json:"applied"`
	NodeID  string       `json:"node_id"`
	TakenAt time.Time    `json:"taken_at"`
}

type engineSnapshot struct {
	root string
	meta snapshotMetadata
}

func (s *engineSnapshot) Persist(sink raft.SnapshotSink) error {
	if s == nil {
		return sink.Cancel()
	}

	tw := tar.NewWriter(sink)
	metaBytes, err := json.Marshal(s.meta)
	if err != nil {
		_ = sink.Cancel()
		return err
	}
	metaHdr := &tar.Header{
		Name:    snapshotMetadataName,
		Mode:    0600,
		Size:    int64(len(metaBytes)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(metaHdr); err != nil {
		_ = sink.Cancel()
		return err
	}
	if _, err := tw.Write(metaBytes); err != nil {
		_ = sink.Cancel()
		return err
	}

	err = filepath.Walk(s.root, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		_, err = io.Copy(tw, file)
		return err
	})
	if err != nil {
		_ = sink.Cancel()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *engineSnapshot) Release() {
	if s == nil || s.root == "" {
		return
	}
	_ = os.RemoveAll(s.root)
}

func extractSnapshot(snapshot io.Reader, dst string) (snapshotMetadata, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return snapshotMetadata{}, err
	}
	var meta snapshotMetadata
	tr := tar.NewReader(snapshot)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return snapshotMetadata{}, err
		}
		target := filepath.Join(dst, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(filepath.Join(dst, snapshotMetadataName)) {
			return snapshotMetadata{}, fmt.Errorf("snapshot path escapes destination: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return snapshotMetadata{}, err
			}
		case tar.TypeReg:
			if filepath.Base(hdr.Name) == snapshotMetadataName {
				data, err := io.ReadAll(tr)
				if err != nil {
					return snapshotMetadata{}, err
				}
				if err := json.Unmarshal(data, &meta); err != nil {
					return snapshotMetadata{}, err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return snapshotMetadata{}, err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(hdr.Mode))
			if err != nil {
				return snapshotMetadata{}, err
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return snapshotMetadata{}, err
			}
			if err := file.Close(); err != nil {
				return snapshotMetadata{}, err
			}
		default:
			return snapshotMetadata{}, fmt.Errorf("snapshot entry %s has unsupported type %d", hdr.Name, hdr.Typeflag)
		}
	}
	return meta, nil
}

func copyTree(dst, src string, skip func(string) bool) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip != nil && skip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = srcFile.Close() }()
		dstFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(dstFile, srcFile); err != nil {
			_ = dstFile.Close()
			return err
		}
		return dstFile.Close()
	})
}

func replaceEngineDirContents(dst, src string) error {
	entries, err := os.ReadDir(dst)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == clusterDirName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return copyTree(dst, src, func(string) bool { return false })
}
