package dbnodes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/monoes/mono-agent/internal/workflow"
)

// mongoPerItemFields are resolved once per input item (n8n evaluates every
// MongoDB parameter per item).
var mongoPerItemFields = []string{"query", "query_parameters", "fields", "update_key", "index_name", "index_definition", "collection", "filter", "update", "document", "pipeline"}

// n8nOnlyMongoOps exist only in the n8n-compatible mode.
var n8nOnlyMongoOps = map[string]bool{
	"delete": true, "insert": true, "update": true, "findOneAndReplace": true, "findOneAndUpdate": true,
	"listSearchIndexes": true, "createSearchIndex": true, "dropSearchIndex": true, "updateSearchIndex": true,
}

func isN8nMongoConfig(config map[string]interface{}) bool {
	return n8nOnlyMongoOps[cfgString(config, "operation")] || !isZeroConfigValue(config["query"]) || cfgBool(config, "n8n_compat", false)
}

// mongoCollection is the slice of a collection the n8n operations use; the
// driver adapter below implements it and tests substitute a fake.
type mongoCollection interface {
	find(ctx context.Context, filter interface{}, limit, skip int64, sort, projection interface{}) ([]map[string]interface{}, error)
	aggregate(ctx context.Context, pipeline interface{}) ([]map[string]interface{}, error)
	deleteMany(ctx context.Context, filter interface{}) (int64, error)
	insertMany(ctx context.Context, docs []interface{}) ([]interface{}, error)
	updateOne(ctx context.Context, filter, update interface{}, upsert bool) error
	replaceOne(ctx context.Context, filter, replacement interface{}, upsert bool) error
	listSearchIndexes(ctx context.Context, name string) ([]map[string]interface{}, error)
	createSearchIndex(ctx context.Context, name, typ string, definition interface{}) (string, error)
	dropSearchIndex(ctx context.Context, name string) error
	updateSearchIndex(ctx context.Context, name string, definition interface{}) error
}

// mongoOpen returns a collection factory and a close func; tests swap it.
var mongoOpen = func(ctx context.Context, uri string) (func(db, coll string) mongoCollection, func(), error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, nil, err
	}
	get := func(db, coll string) mongoCollection { return driverCollection{client.Database(db).Collection(coll)} }
	return get, func() { _ = client.Disconnect(context.Background()) }, nil
}

type driverCollection struct{ c *mongo.Collection }

func drainCursor(ctx context.Context, cur *mongo.Cursor) ([]map[string]interface{}, error) {
	defer cur.Close(ctx)
	var out []map[string]interface{}
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		m, _ := mongoToJSON(doc).(map[string]interface{})
		out = append(out, m)
	}
	return out, cur.Err()
}

func (d driverCollection) find(ctx context.Context, filter interface{}, limit, skip int64, sort, projection interface{}) ([]map[string]interface{}, error) {
	o := options.Find()
	if limit > 0 {
		o.SetLimit(limit)
	}
	if skip > 0 {
		o.SetSkip(skip)
	}
	if sort != nil {
		o.SetSort(sort)
	}
	if projection != nil {
		o.SetProjection(projection)
	}
	cur, err := d.c.Find(ctx, filter, o)
	if err != nil {
		return nil, err
	}
	return drainCursor(ctx, cur)
}

func (d driverCollection) aggregate(ctx context.Context, pipeline interface{}) ([]map[string]interface{}, error) {
	cur, err := d.c.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	return drainCursor(ctx, cur)
}

func (d driverCollection) deleteMany(ctx context.Context, filter interface{}) (int64, error) {
	res, err := d.c.DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (d driverCollection) insertMany(ctx context.Context, docs []interface{}) ([]interface{}, error) {
	res, err := d.c.InsertMany(ctx, docs)
	if err != nil {
		return nil, err
	}
	return res.InsertedIDs, nil
}

func (d driverCollection) updateOne(ctx context.Context, filter, update interface{}, upsert bool) error {
	_, err := d.c.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(upsert))
	return err
}

func (d driverCollection) replaceOne(ctx context.Context, filter, replacement interface{}, upsert bool) error {
	err := d.c.FindOneAndReplace(ctx, filter, replacement, options.FindOneAndReplace().SetUpsert(upsert)).Err()
	if err == mongo.ErrNoDocuments {
		return nil
	}
	return err
}

func (d driverCollection) listSearchIndexes(ctx context.Context, name string) ([]map[string]interface{}, error) {
	o := options.SearchIndexes()
	if name != "" {
		o.SetName(name)
	}
	cur, err := d.c.SearchIndexes().List(ctx, o)
	if err != nil {
		return nil, err
	}
	return drainCursor(ctx, cur)
}

func (d driverCollection) createSearchIndex(ctx context.Context, name, typ string, definition interface{}) (string, error) {
	o := options.SearchIndexes().SetName(name)
	if typ != "" {
		o.SetType(typ)
	}
	return d.c.SearchIndexes().CreateOne(ctx, mongo.SearchIndexModel{Definition: definition, Options: o})
}

func (d driverCollection) dropSearchIndex(ctx context.Context, name string) error {
	return d.c.SearchIndexes().DropOne(ctx, name)
}

func (d driverCollection) updateSearchIndex(ctx context.Context, name string, definition interface{}) error {
	return d.c.SearchIndexes().UpdateOne(ctx, name, definition)
}

// executeMongoN8n runs the n8n-compatible operations once per input item.
func executeMongoN8n(ctx context.Context, op, connStr, database string, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	get, closeFn, err := mongoOpen(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("db.mongodb: connect failed: %w", err)
	}
	defer closeFn()
	var out []workflow.Item
	for i, item := range inputItems(input) {
		cfg, err := resolveForItem(ctx, config, mongoPerItemFields, item, input)
		if err != nil {
			return nil, fmt.Errorf("db.mongodb: %w", err)
		}
		collection := cfgString(cfg, "collection")
		if collection == "" {
			return nil, fmt.Errorf("db.mongodb: 'collection' is required")
		}
		res, err := runMongoOp(ctx, get(database, collection), op, cfg, item)
		if err != nil {
			return nil, fmt.Errorf("db.mongodb: %s (item %d): %w", op, i, err)
		}
		out = append(out, res...)
	}
	return mainOutputs(out, nil), nil
}

func runMongoOp(ctx context.Context, c mongoCollection, op string, cfg map[string]interface{}, item workflow.Item) ([]workflow.Item, error) {
	asItems := func(docs []map[string]interface{}) []workflow.Item {
		items := make([]workflow.Item, 0, len(docs))
		for _, d := range docs {
			items = append(items, workflow.NewItem(d))
		}
		return items
	}
	switch op {
	case "find":
		filter, err := mongoQuery(cfg, "query", "{}")
		if err != nil {
			return nil, err
		}
		limit, _ := cfgInt(cfg, "limit")
		skip, _ := cfgInt(cfg, "skip")
		sort, err := optionalExtJSON(cfg, "sort")
		if err != nil {
			return nil, err
		}
		proj, err := optionalExtJSON(cfg, "projection")
		if err != nil {
			return nil, err
		}
		docs, err := c.find(ctx, filter, int64(limit), int64(skip), sort, proj)
		return asItems(docs), err

	case "aggregate":
		pipeline, err := mongoQuery(cfg, "query", "[]")
		if err != nil {
			return nil, err
		}
		docs, err := c.aggregate(ctx, pipeline)
		return asItems(docs), err

	case "delete":
		filter, err := mongoQuery(cfg, "query", "")
		if err != nil {
			return nil, err
		}
		n, err := c.deleteMany(ctx, filter)
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{"deletedCount": n})}, nil

	case "insert":
		doc, err := prepareMongoDoc(cfg, item, "", true)
		if err != nil {
			return nil, err
		}
		ids, err := c.insertMany(ctx, []interface{}{doc})
		if err != nil {
			return nil, err
		}
		out := mongoToJSON(doc).(map[string]interface{})
		if len(ids) > 0 {
			out["id"] = mongoToJSON(ids[0])
		}
		return []workflow.Item{workflow.NewItem(out)}, nil

	case "update", "findOneAndUpdate", "findOneAndReplace":
		key := cfgString(cfg, "update_key")
		if key == "" {
			return nil, fmt.Errorf("'update_key' is required")
		}
		doc, err := prepareMongoDoc(cfg, item, key, op == "findOneAndReplace")
		if err != nil {
			return nil, err
		}
		filter, err := mongoKeyFilter(doc, key)
		if err != nil {
			return nil, err
		}
		upsert := cfgBool(cfg, "upsert", false)
		if op == "findOneAndReplace" {
			err = c.replaceOne(ctx, filter, doc, upsert)
		} else {
			err = c.updateOne(ctx, filter, bson.M{"$set": doc}, upsert)
		}
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(mongoToJSON(doc).(map[string]interface{}))}, nil

	case "listSearchIndexes":
		docs, err := c.listSearchIndexes(ctx, cfgString(cfg, "index_name"))
		return asItems(docs), err

	case "createSearchIndex", "updateSearchIndex", "dropSearchIndex":
		name := cfgString(cfg, "index_name")
		if name == "" {
			return nil, fmt.Errorf("'index_name' is required")
		}
		if op == "dropSearchIndex" {
			if err := c.dropSearchIndex(ctx, name); err != nil {
				return nil, err
			}
			return []workflow.Item{workflow.NewItem(map[string]interface{}{"indexName": name, "dropped": true})}, nil
		}
		def, err := optionalExtJSON(cfg, "index_definition")
		if err != nil || def == nil {
			return nil, fmt.Errorf("'index_definition' must be a JSON object: %v", err)
		}
		if op == "updateSearchIndex" {
			if err := c.updateSearchIndex(ctx, name, def); err != nil {
				return nil, err
			}
			return []workflow.Item{workflow.NewItem(map[string]interface{}{"indexName": name, "updated": true})}, nil
		}
		created, err := c.createSearchIndex(ctx, name, cfgString(cfg, "index_type"), def)
		if err != nil {
			return nil, err
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{"indexName": created})}, nil
	}
	return nil, fmt.Errorf("unknown operation %q", op)
}

var mongoPlaceholder = regexp.MustCompile(`^\$([1-9][0-9]*)$`)

// mongoQuery parses cfg[key] (Extended JSON text, or a value a template
// already decoded) into BSON, keeping key order, then substitutes "$1".."$N"
// strings (values or keys) with query_parameters. def is used when the key is
// blank; "" means the key is required.
func mongoQuery(cfg map[string]interface{}, key, def string) (interface{}, error) {
	raw := cfg[key]
	if isZeroConfigValue(raw) {
		if def == "" {
			return nil, fmt.Errorf("'%s' is required", key)
		}
		raw = def
	}
	text, ok := raw.(string)
	if !ok {
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("'%s': %w", key, err)
		}
		text = string(b)
	}
	// Wrapping lets one decoder handle both objects and arrays.
	var wrapped struct {
		V interface{} `bson:"v"`
	}
	if err := bson.UnmarshalExtJSON([]byte(`{"v":`+text+`}`), false, &wrapped); err != nil {
		return nil, fmt.Errorf("'%s' is not valid JSON: %w", key, err)
	}
	v := wrapped.V
	switch v.(type) {
	case bson.D, bson.A:
	default:
		return nil, fmt.Errorf("'%s' must be a JSON object or array", key)
	}
	params, err := toList(cfg["query_parameters"])
	if err != nil {
		return nil, fmt.Errorf("'query_parameters': %w", err)
	}
	if len(params) == 0 {
		return v, nil
	}
	return substituteMongoParams(v, params)
}

func mongoParam(s string, params []interface{}) (interface{}, bool, error) {
	m := mongoPlaceholder.FindStringSubmatch(s)
	if m == nil {
		return nil, false, nil
	}
	n, _ := strconv.Atoi(m[1])
	if n > len(params) {
		return nil, false, fmt.Errorf("placeholder %s has no matching query parameter", s)
	}
	return params[n-1], true, nil
}

func substituteMongoParams(v interface{}, params []interface{}) (interface{}, error) {
	switch t := v.(type) {
	case string:
		p, ok, err := mongoParam(t, params)
		if err != nil || !ok {
			return t, err
		}
		return p, nil
	case bson.D:
		out := make(bson.D, len(t))
		for i, e := range t {
			k := e.Key
			if p, ok, err := mongoParam(k, params); err != nil {
				return nil, err
			} else if ok {
				k = fmt.Sprint(p)
			}
			ne, err := substituteMongoParams(e.Value, params)
			if err != nil {
				return nil, err
			}
			out[i] = bson.E{Key: k, Value: ne}
		}
		return out, nil
	case bson.A:
		out := make(bson.A, len(t))
		for i, e := range t {
			ne, err := substituteMongoParams(e, params)
			if err != nil {
				return nil, err
			}
			out[i] = ne
		}
		return out, nil
	}
	return v, nil
}

func optionalExtJSON(cfg map[string]interface{}, key string) (interface{}, error) {
	if isZeroConfigValue(cfg[key]) {
		return nil, nil
	}
	return mongoQuery(map[string]interface{}{key: cfg[key]}, key, "")
}

// prepareMongoDoc picks 'fields' (comma list; all item fields when blank)
// from the item, honouring use_dot_notation and date_fields. The update key
// is always included.
func prepareMongoDoc(cfg map[string]interface{}, item workflow.Item, updateKey string, nest bool) (map[string]interface{}, error) {
	fields := toStringList(cfg["fields"])
	dotted := cfgBool(cfg, "use_dot_notation", false)
	dates := map[string]bool{}
	for _, f := range toStringList(cfg["date_fields"]) {
		dates[f] = true
	}
	if len(fields) == 0 {
		for k := range item.JSON {
			fields = append(fields, k)
		}
	}
	if updateKey != "" {
		if _, ok := lookupPath(item.JSON, updateKey, dotted); !ok {
			return nil, fmt.Errorf("item is missing the update key field %q", updateKey)
		}
		found := false
		for _, f := range fields {
			found = found || f == updateKey
		}
		if !found {
			fields = append(fields, updateKey)
		}
	}
	doc := map[string]interface{}{}
	for _, f := range fields {
		v, _ := lookupPath(item.JSON, f, dotted)
		if s, ok := v.(string); ok && dates[f] && s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return nil, fmt.Errorf("date field %q: %w", f, err)
			}
			v = t
		}
		if f == updateKey {
			switch v.(type) {
			case map[string]interface{}, []interface{}:
				return nil, fmt.Errorf("the value of %q must be a string, number, boolean or date", updateKey)
			}
		}
		if dotted && nest && strings.Contains(f, ".") {
			setPath(doc, f, v)
		} else {
			doc[f] = v
		}
	}
	return doc, nil
}

func mongoKeyFilter(doc map[string]interface{}, key string) (bson.M, error) {
	v := doc[key]
	if key == "_id" {
		delete(doc, "_id")
		if s, ok := v.(string); ok {
			oid, err := bson.ObjectIDFromHex(s)
			if err != nil {
				return nil, fmt.Errorf("_id %q is not an ObjectId: %w", s, err)
			}
			return bson.M{"_id": oid}, nil
		}
	}
	return bson.M{key: v}, nil
}

func lookupPath(m map[string]interface{}, path string, dotted bool) (interface{}, bool) {
	if !dotted {
		v, ok := m[path]
		return v, ok
	}
	var cur interface{} = m
	for _, p := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func setPath(m map[string]interface{}, path string, v interface{}) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

// mongoToJSON converts BSON values into plain JSON-friendly Go values.
func mongoToJSON(v interface{}) interface{} {
	switch t := v.(type) {
	case bson.D:
		m := make(map[string]interface{}, len(t))
		for _, e := range t {
			m[e.Key] = mongoToJSON(e.Value)
		}
		return m
	case bson.M:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = mongoToJSON(e)
		}
		return m
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = mongoToJSON(e)
		}
		return m
	case bson.A:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = mongoToJSON(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = mongoToJSON(e)
		}
		return out
	case bson.ObjectID:
		return t.Hex()
	case bson.DateTime:
		return t.Time().UTC().Format(time.RFC3339Nano)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case bson.Decimal128:
		return t.String()
	}
	return v
}
