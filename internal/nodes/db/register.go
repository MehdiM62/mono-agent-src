package dbnodes

import "github.com/monoes/mono-agent/internal/workflow"

// RegisterAll registers the database, queue and directory node types.
func RegisterAll(r *workflow.NodeTypeRegistry) {
	r.Register("db.mysql", func() workflow.NodeExecutor { return &MySQLNode{} })
	r.Register("db.postgres", func() workflow.NodeExecutor { return &PostgresNode{} })
	r.Register("db.mongodb", func() workflow.NodeExecutor { return &MongoDBNode{} })
	r.Register("db.redis", func() workflow.NodeExecutor { return &RedisNode{} })

	r.Register("db.mssql", NewMSSQLNode)
	r.Register("db.oracle", NewOracleNode)
	r.Register("db.snowflake", func() workflow.NodeExecutor { return &SnowflakeNode{} })
	r.Register("db.timescaledb", NewTimescaleDBNode)
	r.Register("db.cratedb", NewCrateDBNode)
	r.Register("db.questdb", NewQuestDBNode)
	r.Register("db.ldap", func() workflow.NodeExecutor { return &LDAPNode{} })

	r.Register("queue.kafka", func() workflow.NodeExecutor { return &KafkaNode{} })
	r.Register("queue.rabbitmq", func() workflow.NodeExecutor { return &RabbitMQNode{} })
	r.Register("queue.amqp", func() workflow.NodeExecutor { return &AMQPNode{} })
	r.Register("queue.mqtt", func() workflow.NodeExecutor { return &MQTTNode{} })
}
