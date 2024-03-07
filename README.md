# Clicky Chats

Clicky Chats is an OpenAI API clone with an extension to chat with tools.

## Components

There are two basic components: `server` and `agent`. The `server` handles the basic CRUD operations and the `agent` is responsible for the orchestration. These can be run by specifying the corresponding subcommand. One can also run the server and agent in the same process by running `clicky-chats server --with-agents`.

## Development

The two components can be run simultaneously for development with the following:
```bash
export OPENAI_API_KEY=<your-api-key>
make run-dev
```

Setting the `CLICKY_CHATS_DEBUG` environment variable to anything will turn on debug logging:
```bash
export CLICKY_CHATS_DEBUG=1
export OPENAI_API_KEY=<your-api-key>
make run-dev
```