package config

import "github.com/ostendorf-cloud/govue/envconf"

// ObjectStoreConfig is the S3 compatible store receipts, originals and
// the logo live in. Issue #75 adds the fields and the loading. The
// variable names are fixed by docs/concepts/solobill.md, "Object store
// client and configuration".
type ObjectStoreConfig struct{}

// loadObjectStore reads the S3_* variables into its second argument and
// reports every
// problem through fail. It reads nothing until issue #75 lands.
func loadObjectStore(_ *envconf.Loader, _ *ObjectStoreConfig, _ func(error)) {}
