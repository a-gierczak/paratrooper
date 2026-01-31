package storage

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

type objectPathTest struct {
	Path string `binding:"asset_path"`
}

func TestAssetPathValidator(t *testing.T) {
	require.NoError(t, RegisterValidators())

	invalid := []objectPathTest{
		{Path: "/bundles/to/object/43290430fds-xvc8zx-ceeqw/asset"},
		{Path: "/assets/to/object/43290430fds-xvc8zx-ceeqw/asset"},
		{Path: "bundles/../../../some/path/outside"},
		{Path: "assets/../../../some/path/outside"},
	}
	for _, v := range invalid {
		require.Error(t, binding.Validator.ValidateStruct(&v), v.Path)
	}

	valid := []objectPathTest{
		{Path: "manifest.json"},
		{Path: "bundles/asset"},
		{Path: "bundles/asset.js"},
		{Path: "bundles/asset.hbc"},
		{Path: "assets/cczcx.js"},
		{Path: "assets/cczcx.png"},
		{Path: "assets/cczcx"},
		{Path: "./bundles/asset"},
		{Path: "./assets/asset"},
		{Path: "other/to/object/43290430fds-xvc8zx-ceeqw/asset"},
		{Path: "other/to/object/43290430fds-xvc8zx-ceeqw/asset"},
		{Path: "bundles\\asset"},
		{Path: "bundles\\asset.js"},
		{Path: "bundles\\asset.hbc"},
		{Path: "assets\\cczcx.js"},
		{Path: "assets\\cczcx.png"},
		{Path: "assets\\cczcx"},
		{Path: ".\\bundles\\asset"},
		{Path: ".\\assets\\asset"},
		{Path: "other\\to\\object\\43290430fds-xvc8zx-ceeqw\\asset"},
		{Path: "other\\to\\object\\43290430fds-xvc8zx-ceeqw\\asset"},
	}
	for _, v := range valid {
		require.NoError(t, binding.Validator.ValidateStruct(&v), v.Path)
	}
}
