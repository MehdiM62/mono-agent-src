package dbnodes

// Schema-only companion structs for queue.* and db.ldap; connection details
// (brokers, host, credentials, TLS) come from the connection (credential_id).

// credential_platform: kafka
//
// KafkaNodeSchema documents queue.kafka.
type KafkaNodeSchema struct {
	Topic         string  `json:"topic" schema:"label=Topic,type=text,required"`
	SendInputData bool    `json:"send_input_data" schema:"label=Send Input Data,type=boolean,default=true,help=Send the item as JSON; off sends Message."`
	Message       string  `json:"message" schema:"label=Message,type=textarea,rows=3,depends_on_key=send_input_data,depends_on_values=false"`
	MessageKey    string  `json:"message_key" schema:"label=Message Key,type=text,help=Optional partitioning key."`
	Headers       string  `json:"headers" schema:"label=Headers (JSON object),type=textarea,rows=2,placeholder={ \"source\": \"monoagent\" }"`
	Acks          bool    `json:"acks" schema:"label=Wait for All Replicas,type=boolean,help=acks=all instead of leader-only."`
	Compression   string  `json:"compression" schema:"label=Compression,type=select,options=none|gzip|snappy|lz4|zstd"`
	Timeout       float64 `json:"timeout" schema:"label=Timeout (ms),type=number"`
}

// credential_platform: rabbitmq
//
// RabbitMQNodeSchema documents queue.rabbitmq.
type RabbitMQNodeSchema struct {
	Mode              string `json:"mode" schema:"label=Send To,type=select,options=queue|exchange,default=queue"`
	Queue             string `json:"queue" schema:"label=Queue,type=text,depends_on_key=mode,depends_on_values=queue"`
	Exchange          string `json:"exchange" schema:"label=Exchange,type=text,depends_on_key=mode,depends_on_values=exchange"`
	ExchangeType      string `json:"exchange_type" schema:"label=Exchange Type,type=select,options=fanout|direct|topic|headers,default=fanout,depends_on_key=mode,depends_on_values=exchange"`
	RoutingKey        string `json:"routing_key" schema:"label=Routing Key,type=text,depends_on_key=mode,depends_on_values=exchange"`
	SendInputData     bool   `json:"send_input_data" schema:"label=Send Input Data,type=boolean,default=true"`
	Message           string `json:"message" schema:"label=Message,type=textarea,rows=3,depends_on_key=send_input_data,depends_on_values=false"`
	Durable           bool   `json:"durable" schema:"label=Durable,type=boolean,default=true"`
	AutoDelete        bool   `json:"auto_delete" schema:"label=Auto Delete,type=boolean"`
	Exclusive         bool   `json:"exclusive" schema:"label=Exclusive,type=boolean,depends_on_key=mode,depends_on_values=queue"`
	AssertQueue       bool   `json:"assert_queue" schema:"label=Declare Queue,type=boolean,default=true,depends_on_key=mode,depends_on_values=queue"`
	AssertExchange    bool   `json:"assert_exchange" schema:"label=Declare Exchange,type=boolean,default=true,depends_on_key=mode,depends_on_values=exchange"`
	AlternateExchange string `json:"alternate_exchange" schema:"label=Alternate Exchange,type=text,depends_on_key=mode,depends_on_values=exchange"`
	Arguments         string `json:"arguments" schema:"label=Arguments (JSON object),type=textarea,rows=2"`
	Headers           string `json:"headers" schema:"label=Headers (JSON object),type=textarea,rows=2"`
}

// credential_platform: amqp
//
// AMQPNodeSchema documents queue.amqp (AMQP 1.0).
type AMQPNodeSchema struct {
	Sink             string `json:"sink" schema:"label=Queue / Topic,type=text,required,placeholder=topic://events"`
	HeaderParameters string `json:"header_parameters" schema:"label=Application Properties (JSON object),type=textarea,rows=2"`
	SendOnlyProperty string `json:"send_only_property" schema:"label=Send Only Property,type=text,help=Send just this item field instead of the whole item."`
	DataAsObject     bool   `json:"data_as_object" schema:"label=Data as Object,type=boolean,help=Send an AMQP value instead of a JSON string body."`
	ContainerID      string `json:"container_id" schema:"label=Container ID,type=text"`
}

// credential_platform: mqtt
//
// MQTTNodeSchema documents queue.mqtt.
type MQTTNodeSchema struct {
	Topic         string  `json:"topic" schema:"label=Topic,type=text,required"`
	SendInputData bool    `json:"send_input_data" schema:"label=Send Input Data,type=boolean,default=true"`
	Message       string  `json:"message" schema:"label=Message,type=textarea,rows=3,depends_on_key=send_input_data,depends_on_values=false"`
	QoS           float64 `json:"qos" schema:"label=QoS,type=select,options=0|1|2,default=0"`
	Retain        bool    `json:"retain" schema:"label=Retain,type=boolean"`
}

// credential_platform: ldap
//
// LDAPNodeSchema documents db.ldap.
type LDAPNodeSchema struct {
	Operation        string  `json:"operation" schema:"label=Operation,type=select,required,options=search|compare|create|update|rename|delete,default=search"`
	DN               string  `json:"dn" schema:"label=DN,type=text,placeholder=cn=ada，ou=people，dc=example，dc=com,depends_on_key=operation,depends_on_values=compare|create|update|rename|delete"`
	TargetDN         string  `json:"target_dn" schema:"label=New DN,type=text,depends_on_key=operation,depends_on_values=rename"`
	AttributeID      string  `json:"attribute_id" schema:"label=Attribute,type=text,depends_on_key=operation,depends_on_values=compare"`
	Value            string  `json:"value" schema:"label=Value,type=text,depends_on_key=operation,depends_on_values=compare"`
	Attributes       string  `json:"attributes" schema:"label=Attributes (JSON),type=textarea,rows=4,placeholder=[{ \"id\": \"mail\"， \"value\": \"a@x\" }],help=create: [{id， value}]. update: {\"add\"|\"replace\"|\"delete\": [{id， value}]}.,depends_on_key=operation,depends_on_values=create|update"`
	BaseDN           string  `json:"base_dn" schema:"label=Base DN,type=text,depends_on_key=operation,depends_on_values=search"`
	SearchFor        string  `json:"search_for" schema:"label=Search For,type=text,placeholder=(objectclass=person),help=An object-class filter fragment， or custom.,depends_on_key=operation,depends_on_values=search"`
	CustomFilter     string  `json:"custom_filter" schema:"label=Custom Filter,type=text,placeholder=(&(objectclass=user)(mail=*)),depends_on_key=search_for,depends_on_values=custom"`
	Attribute        string  `json:"attribute" schema:"label=Attribute to Match,type=text,placeholder=cn,depends_on_key=operation,depends_on_values=search"`
	SearchText       string  `json:"search_text" schema:"label=Search Text,type=text,help=* is a wildcard; other filter characters are escaped.,depends_on_key=operation,depends_on_values=search"`
	ReturnAll        bool    `json:"return_all" schema:"label=Return All,type=boolean,depends_on_key=operation,depends_on_values=search"`
	Limit            float64 `json:"limit" schema:"label=Limit,type=number,default=50,depends_on_key=operation,depends_on_values=search"`
	ReturnAttributes string  `json:"return_attributes" schema:"label=Attributes to Return,type=text,placeholder=cn， mail,depends_on_key=operation,depends_on_values=search"`
	PageSize         float64 `json:"page_size" schema:"label=Page Size,type=number,default=1000,depends_on_key=operation,depends_on_values=search"`
	Scope            string  `json:"scope" schema:"label=Scope,type=select,options=sub|one|base,default=sub,depends_on_key=operation,depends_on_values=search"`
}
