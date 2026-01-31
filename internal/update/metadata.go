package update

import (
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin/binding"
)

type Metadata struct {
	Version      int                     `json:"version"`
	Bundler      string                  `json:"bundler"`
	FileMetadata map[string]FileMetadata `json:"fileMetadata" binding:"dive"`
}

type FileMetadata struct {
	Bundle string              `json:"bundle" binding:"required"`
	Assets []FileMetadataAsset `json:"assets" binding:"dive"`
}

type FileMetadataAsset struct {
	Path string `json:"path" binding:"required,asset_path,max=1024"`
	Ext  string `json:"ext"  binding:"required,asset_ext,max=16"`
}

func ParseMetadata(r io.Reader) (*Metadata, error) {
	var metadata Metadata
	if err := json.NewDecoder(r).Decode(&metadata); err != nil {
		return nil, err
	}

	err := binding.Validator.ValidateStruct(&metadata)
	if err != nil {
		return nil, err
	}

	return &metadata, nil
}
