//go:build !sqlitevec_cgo

package sqlitevec

func newProvider(cfg Config) (vectorProvider, error) {
	reason := "sqlite-vec support disabled"
	if cfg.EnableSQLiteVec {
		reason = "sqlite-vec build tag not enabled; rebuild with -tags sqlitevec_cgo"
	}
	return disabledProvider{reason: reason}, nil
}
