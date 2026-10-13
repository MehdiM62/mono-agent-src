package dbnodes

import (
	"context"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/monoes/mono-agent/internal/workflow"
)

type mongoCall struct {
	op                    string
	coll                  string
	filter, update, extra interface{}
	upsert                bool
	limit, skip           int64
}

type fakeMongo struct {
	coll  string
	calls *[]mongoCall
	docs  []map[string]interface{}
}

func (f fakeMongo) rec(c mongoCall) { c.coll = f.coll; *f.calls = append(*f.calls, c) }
func (f fakeMongo) find(_ context.Context, filter interface{}, limit, skip int64, sort, proj interface{}) ([]map[string]interface{}, error) {
	f.rec(mongoCall{op: "find", filter: filter, limit: limit, skip: skip, update: sort, extra: proj})
	return f.docs, nil
}
func (f fakeMongo) aggregate(_ context.Context, p interface{}) ([]map[string]interface{}, error) {
	f.rec(mongoCall{op: "aggregate", filter: p})
	return f.docs, nil
}
func (f fakeMongo) deleteMany(_ context.Context, filter interface{}) (int64, error) {
	f.rec(mongoCall{op: "deleteMany", filter: filter})
	return 3, nil
}
func (f fakeMongo) insertMany(_ context.Context, docs []interface{}) ([]interface{}, error) {
	f.rec(mongoCall{op: "insertMany", extra: docs})
	return []interface{}{bson.ObjectID{1}}, nil
}
func (f fakeMongo) updateOne(_ context.Context, filter, update interface{}, upsert bool) error {
	f.rec(mongoCall{op: "updateOne", filter: filter, update: update, upsert: upsert})
	return nil
}
func (f fakeMongo) replaceOne(_ context.Context, filter, repl interface{}, upsert bool) error {
	f.rec(mongoCall{op: "replaceOne", filter: filter, update: repl, upsert: upsert})
	return nil
}
func (f fakeMongo) listSearchIndexes(_ context.Context, name string) ([]map[string]interface{}, error) {
	f.rec(mongoCall{op: "listSearchIndexes", extra: name})
	return f.docs, nil
}
func (f fakeMongo) createSearchIndex(_ context.Context, name, typ string, def interface{}) (string, error) {
	f.rec(mongoCall{op: "createSearchIndex", extra: name + "/" + typ, update: def})
	return name, nil
}
func (f fakeMongo) dropSearchIndex(_ context.Context, name string) error {
	f.rec(mongoCall{op: "dropSearchIndex", extra: name})
	return nil
}
func (f fakeMongo) updateSearchIndex(_ context.Context, name string, def interface{}) error {
	f.rec(mongoCall{op: "updateSearchIndex", extra: name, update: def})
	return nil
}

func withFakeMongo(t *testing.T, docs []map[string]interface{}) *[]mongoCall {
	t.Helper()
	calls := &[]mongoCall{}
	prev := mongoOpen
	mongoOpen = func(context.Context, string) (func(string, string) mongoCollection, func(), error) {
		return func(db, coll string) mongoCollection {
			return fakeMongo{coll: db + "." + coll, calls: calls, docs: docs}
		}, func() {}, nil
	}
	t.Cleanup(func() { mongoOpen = prev })
	return calls
}

func runMongo(t *testing.T, input workflow.NodeInput, cfg map[string]interface{}) []workflow.Item {
	t.Helper()
	base := map[string]interface{}{"connection_string": "mongodb://x", "database": "app", "collection": "users"}
	for k, v := range cfg {
		base[k] = v
	}
	out, err := (&MongoDBNode{}).Execute(context.Background(), input, base)
	if err != nil {
		t.Fatal(err)
	}
	return mainItems(t, out)
}

func TestMongoFindWithParametersAndOptions(t *testing.T) {
	calls := withFakeMongo(t, []map[string]interface{}{{"name": "Ada"}})
	got := runMongo(t, workflow.NodeInput{}, map[string]interface{}{
		"operation": "find", "query": `{"age": {"$gt": "$1"}, "_id": {"$oid": "000000000000000000000001"}}`,
		"query_parameters": []interface{}{float64(30)}, "limit": float64(5), "skip": float64(2), "sort": `{"age": -1}`,
	})
	if len(got) != 1 || len(*calls) != 1 {
		t.Fatalf("got %v calls %v", got, *calls)
	}
	c := (*calls)[0]
	if c.coll != "app.users" || c.limit != 5 || c.skip != 2 {
		t.Errorf("call = %+v", c)
	}
	f := c.filter.(bson.D)
	if f[0].Key != "age" || f[0].Value.(bson.D)[0].Value != float64(30) {
		t.Errorf("filter = %#v", f)
	}
	if _, ok := f[1].Value.(bson.ObjectID); !ok {
		t.Errorf("$oid not decoded: %#v", f[1].Value)
	}
}

func TestMongoAggregateAndDelete(t *testing.T) {
	calls := withFakeMongo(t, nil)
	runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "aggregate", "query": `[{"$match": {"a": 1}}]`})
	got := runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "delete", "query": `{"a": 1}`})
	if _, ok := (*calls)[0].filter.(bson.A); !ok {
		t.Errorf("pipeline type = %T", (*calls)[0].filter)
	}
	if got[0].JSON["deletedCount"] != int64(3) {
		t.Errorf("delete out = %v", got)
	}
	if _, err := (&MongoDBNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"connection_string": "mongodb://x", "database": "app", "collection": "c", "operation": "delete",
	}); err == nil {
		t.Error("delete without query: want error")
	}
}

func TestMongoInsertAndUpdatePerItem(t *testing.T) {
	calls := withFakeMongo(t, nil)
	in := items(map[string]interface{}{"name": "Ada", "age": 36, "x": 1}, map[string]interface{}{"name": "Bob", "age": 40})
	got := runMongo(t, in, map[string]interface{}{"operation": "insert", "fields": "name,age"})
	if len(*calls) != 2 || len(got) != 2 || got[0].JSON["id"] != (bson.ObjectID{1}).Hex() {
		t.Fatalf("insert got %v calls %v", got, *calls)
	}
	if doc := (*calls)[0].extra.([]interface{})[0].(map[string]interface{}); len(doc) != 2 {
		t.Errorf("insert doc = %v", doc)
	}

	*calls = nil
	runMongo(t, items(map[string]interface{}{"email": "a@x", "name": "Ada"}), map[string]interface{}{
		"operation": "update", "update_key": "email", "fields": "name", "upsert": true,
	})
	c := (*calls)[0]
	if c.op != "updateOne" || !c.upsert || !reflect.DeepEqual(c.filter, bson.M{"email": "a@x"}) {
		t.Errorf("update call = %+v", c)
	}
	if set := c.update.(bson.M)["$set"].(map[string]interface{}); set["name"] != "Ada" {
		t.Errorf("update doc = %v", c.update)
	}

	*calls = nil
	runMongo(t, items(map[string]interface{}{"_id": "000000000000000000000001", "name": "Ada"}), map[string]interface{}{
		"operation": "findOneAndReplace", "update_key": "_id",
	})
	c = (*calls)[0]
	if _, ok := c.filter.(bson.M)["_id"].(bson.ObjectID); !ok || c.op != "replaceOne" {
		t.Errorf("replace call = %+v", c)
	}
	if _, has := c.update.(map[string]interface{})["_id"]; has {
		t.Error("_id must be removed from the replacement document")
	}
}

func TestMongoUpdateRequiresKeyOnItem(t *testing.T) {
	withFakeMongo(t, nil)
	_, err := (&MongoDBNode{}).Execute(context.Background(), items(map[string]interface{}{"name": "Ada"}), map[string]interface{}{
		"connection_string": "mongodb://x", "database": "app", "collection": "c", "operation": "update", "update_key": "email",
	})
	if err == nil {
		t.Fatal("want error for missing update key")
	}
}

func TestMongoSearchIndexOps(t *testing.T) {
	calls := withFakeMongo(t, []map[string]interface{}{{"name": "idx"}})
	runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "createSearchIndex", "index_name": "idx", "index_type": "vectorSearch", "index_definition": `{"fields": []}`})
	runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "updateSearchIndex", "index_name": "idx", "index_definition": `{"mappings": {"dynamic": true}}`})
	got := runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "listSearchIndexes"})
	runMongo(t, workflow.NodeInput{}, map[string]interface{}{"operation": "dropSearchIndex", "index_name": "idx"})
	ops := []string{}
	for _, c := range *calls {
		ops = append(ops, c.op)
	}
	if !reflect.DeepEqual(ops, []string{"createSearchIndex", "updateSearchIndex", "listSearchIndexes", "dropSearchIndex"}) || len(got) != 1 {
		t.Errorf("ops = %v got = %v", ops, got)
	}
	if (*calls)[0].extra != "idx/vectorSearch" {
		t.Errorf("create args = %v", (*calls)[0].extra)
	}
}

func TestMongoToJSONConvertsBSON(t *testing.T) {
	oid := bson.ObjectID{2}
	got := mongoToJSON(bson.D{{Key: "_id", Value: oid}, {Key: "tags", Value: bson.A{"a", bson.D{{Key: "b", Value: 1}}}}})
	want := map[string]interface{}{"_id": oid.Hex(), "tags": []interface{}{"a", map[string]interface{}{"b": 1}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}
