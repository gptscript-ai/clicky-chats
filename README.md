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

## Extending the OpenAI API

There are three separate APIs served here:
- `/v1` servers a copy of the OpenAI API
- `/v1/rubra` serves an extended OpenAI API, that is, the same objects with additional fields added. All objects from OpenAI that aren't extended can are also served here.
- `/v1/rubra/x` serves our own APIs that are used in conjunction with the above.

To add extra fields to existing OpenAI APIs, the `GetExtendedAPIs` in the `extendedapi` package is used. The current example in that package is adding the `gptscript_tools` field to the OpenAI Assistant object. In order to do that, we must add that field to the `CreateAssistantObject`, `ModifyAssistantObject` and the `AssistantObject`. The generator will add any additional fields to the existing OpenAI objects for the extension API.

To add net-new APIs, paths and components are added to the `pkg/generated/rubrax.yaml` OpenAPI spec. This spec will be combined with the OpenAI API and extended specs to create one unified spec that is used to generate the types and server.