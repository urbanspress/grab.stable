package data

import (
	"errors"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

func driverError(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 255 {
		case 18, 9:
			return fail(Limit, "database deadline or native resource limit exceeded")
		case 5, 6:
			return fail(Operation, "database conflict")
		case 19:
			return fail(Operation, "database constraint rejected the operation")
		case 20, 25:
			return fail(Type, "database rejected a value's type or range")
		}
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1205, 1213:
			return fail(Operation, "database conflict")
		case 1062, 1451, 1452:
			return fail(Operation, "database constraint rejected the operation")
		case 1264, 1265, 1292:
			return fail(Type, "database rejected a value's type or range")
		case 1317, 1969, 3024:
			return fail(Limit, "database deadline exceeded")
		}
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "57014":
			return fail(Limit, "database deadline exceeded")
		case "22001", "22003", "22007", "22008":
			return fail(Type, "database rejected a value's type or range")
		}
		if len(pe.Code) >= 2 {
			switch pe.Code[:2] {
			case "23":
				return fail(Operation, "database constraint rejected the operation")
			case "40":
				return fail(Operation, "database conflict")
			}
		}
	}
	return fail(Operation, "database operation unavailable or failed")
}
