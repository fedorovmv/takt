# External executor example

Запусти `takt mcp --surface all --workspace . --config examples/external-executor/config.yaml`, затем через MCP вызови `takt.run.start` для `examples/external-executor/workflow.yaml`. Внешний coding agent получает узел через `takt.node.pending`, заявляет его через `takt.node.claim`, пишет события и завершает `takt.node.complete` либо `takt.node.fail`.
