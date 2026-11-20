package storage

// ImportRepository is reserved for the historic import (issue #81, I13).
// Its methods depend on the import format, which the owner decides
// later, so the method set is empty until then. Issue #81 fills it in
// this file and nowhere else.
type ImportRepository interface{}
