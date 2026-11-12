package data

import "strings"

// Check available native metadata even when a query returns no rows. Unknown
// SQLite expression types are checked against actual storage on every row.
func compatibleColumn(driver, native string, spec TypeSpec) bool {
	native = strings.ToUpper(native)
	if native == "" {
		return driver == "sqlite"
	}
	if driver == "sqlite" {
		if spec.Type == "decimal" {
			return strings.Contains(native, "TEXT") || strings.Contains(native, "CHAR") || strings.Contains(native, "CLOB")
		}
		return true
	}
	if driver == "postgres" {
		switch spec.Type {
		case "integer":
			return native == "INT2" || native == "INT4" || native == "INT8" || native == "NUMERIC"
		case "decimal":
			return native == "NUMERIC"
		case "boolean":
			return native == "BOOL"
		case "text":
			return native == "TEXT" || native == "VARCHAR" || native == "BPCHAR" || native == "NAME" || native == "CHAR" || native == "DATE" || native == "TIME"
		case "bytes":
			return native == "BYTEA"
		case "float":
			return native == "FLOAT4" || native == "FLOAT8"
		case "timestamp":
			return spec.Zone == "utc" && native == "TIMESTAMPTZ" || spec.Zone == "naive" && native == "TIMESTAMP"
		}
		return false
	}
	native = strings.TrimPrefix(native, "UNSIGNED ")
	switch spec.Type {
	case "integer":
		return native == "TINYINT" || native == "SMALLINT" || native == "MEDIUMINT" || native == "INT" || native == "BIGINT"
	case "decimal":
		return native == "DECIMAL"
	case "boolean":
		return native == "TINYINT"
	case "text":
		return native == "CHAR" || native == "VARCHAR" || native == "TEXT" || native == "TINYTEXT" || native == "MEDIUMTEXT" || native == "LONGTEXT" || native == "DATE" || native == "TIME"
	case "bytes":
		return native == "BLOB" || native == "TINYBLOB" || native == "MEDIUMBLOB" || native == "LONGBLOB" || native == "BINARY" || native == "VARBINARY"
	case "float":
		return native == "FLOAT" || native == "DOUBLE"
	case "timestamp":
		return spec.Zone == "utc" && native == "TIMESTAMP" || spec.Zone == "naive" && native == "DATETIME"
	}
	return false
}
