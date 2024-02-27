# Clicky Chats

Clicky Chats is an OpenAI API clone with an extension to chat with tools.

## Components

There are two basic components: `server` and `agent`. The `server` handles the basic CRUD operations and the `agent` is responsible for the orchestration. These can be run by specifying the corresponding subcommand. One can also run the server and agent in the same process by running `clicky-chats server --with-agent`.