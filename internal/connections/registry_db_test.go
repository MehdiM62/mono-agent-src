package connections

import "testing"

// dbPlatformIDs are the database, queue and directory platforms the db.* and
// queue.* nodes use beyond the original four databases.
var dbPlatformIDs = []string{"mssql", "oracle", "timescaledb", "cratedb", "questdb", "snowflake", "ldap", "kafka", "rabbitmq", "amqp", "mqtt"}

func TestDBPlatformsRegistered(t *testing.T) {
	for _, id := range dbPlatformIDs {
		p, ok := Registry[id]
		if !ok {
			t.Errorf("Registry missing platform %q", id)
			continue
		}
		if p.ID != id || len(p.Methods) != 1 || len(p.Fields[p.Methods[0]]) == 0 {
			t.Errorf("platform %q is malformed: %+v", id, p)
		}
	}
}
