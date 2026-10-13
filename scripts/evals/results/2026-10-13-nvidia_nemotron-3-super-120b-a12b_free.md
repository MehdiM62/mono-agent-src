# monoagentcli capability evals: nvidia/nemotron-3-super-120b-a12b:free

Started 20261013-154603, 161.6s total, binary `/home/monoes/scratch/rw-evals-bin/monoagentcli`.

| task | mode | result | checks | steps | seconds |
|---|---|---|---|---|---|
| http-filter-csv | cli | FAIL 3/5 | validate_ok:PASS dry_run_ok:PASS run_status:FAIL node_types:PASS file:FAIL | 16 | 72.9 |
| rss-read | cli | FAIL 4/7 | validate_ok:PASS dry_run_ok:PASS run_status:FAIL mcp_validate_ok:PASS mcp_run_status:FAIL node_types:PASS output:FAIL | 9 | 47.8 |
| schedule-daily | cli | PASS 5/5 | validate_ok:PASS dry_run_ok:PASS node_types:PASS node_config:PASS inactive:PASS | 7 | 21.3 |

Tasks fully passed: 1/3. Checks passed: 12/17.

## Failed checks

- http-filter-csv (cli) run_status: status FAILED, want SUCCESS; non-success nodes: n4:FAILED
- http-filter-csv (cli) file: cannot read /home/monoes/scratch/monoagent-evals-20261013-154603/http-filter-csv-cli-k6vu5m84/out/expensive.csv: No such file or directory
- rss-read (cli) run_status: status FAILED, want SUCCESS; non-success nodes: n3:FAILED
- rss-read (cli) mcp_run_status: status FAILED: "{\n  \"error\": \"node n3 (Keep First 2): limit: 'max_items' config is required\",\n  \"execution_id\": \"0f603f9f-0b99-40d7-88e3-eeb995722711\",\n  \"nodes\":...
- rss-read (cli) output: got ["Alpha release", "Beta notes", "Gamma launch"], want ["Alpha release", "Beta notes"]
