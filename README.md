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

If you need to create assistants that use the `retrieval` tool, you will also need to run the `knowledge-retrieval-api` service. See the Complimentary Services section for more information.
In that case, you need to `export CLICKY_CHATS_KNOWLEDGE_RETRIEVAL_API_URL=http://localhost:8000` before starting clicky-chats.

Setting the `CLICKY_CHATS_DEBUG` environment variable to anything will turn on debug logging:

```bash
export CLICKY_CHATS_DEBUG=1
export OPENAI_API_KEY=<your-api-key>
make run-dev
```

### Complimentary Services

#### Rubra UI

The Rubra UI is a simple web interface that can be used to interact with the server.

**Repository**: <https://github.com/acorn-io/rubra-ui>

##### Requirements

- NodeJS
- Yarn

##### Setup

1. Install dependencies:

    ```bash
    yarn
    ```

2. Start the server:

    ```bash
    export NUXT_API=http://localhost:8080/v1 # this points back to the clicky-chats server
    export NUXT_API_KEY=sk-foobar # required to be set for the frontend, but not used at the moment
    yarn dev
    ```

#### Knowledge Retrieval API

The knowledge retrieval API is a simple API backed by a Vector Database that allows you to augment assistant's answers with information retrieved from documents you add to it. This is a requirement to use the `retrieval` tool when creating a new assistant.

**Repository**:<https://github.com/gptscript-ai/knowledge-retrieval-api>

##### Requirements

- Python 3.10+
- Docker & Docker Compose (for running the pgvector database)

##### Setup

1. Setup a Python virtual environment and install dependencies:

    ```bash
    python3 -m venv venv
    source venv/bin/activate
    pip install -r requirements.txt
    ```

2. Start the server (hot-reloading code) and database:

    ```bash
    make run-dev
    ```

##### Note

You have to start clicky-chats with the following environment variable pointing to the knowledge-retrieval-api:

```bash
export CLICKY_CHATS_KNOWLEDGE_RETRIEVAL_API_URL=http://localhost:8000
```

## Extending the OpenAI API

There are three separate APIs served here:

- `/v1` servers a copy of the OpenAI API
- `/v1/rubra` serves an extended OpenAI API, that is, the same objects with additional fields added. All objects from OpenAI that aren't extended can are also served here.
- `/v1/rubra/x` serves our own APIs that are used in conjunction with the above.

To add extra fields to existing OpenAI APIs, the `GetExtendedAPIs` in the `extendedapi` package is used. The current example in that package is adding the `gptscript_tools` field to the OpenAI Assistant object. In order to do that, we must add that field to the `CreateAssistantObject`, `ModifyAssistantObject` and the `AssistantObject`. The generator will add any additional fields to the existing OpenAI objects for the extension API.

To add net-new APIs, paths and components are added to the `pkg/generated/rubrax.yaml` OpenAPI spec. This spec will be combined with the OpenAI API and extended specs to create one unified spec that is used to generate the types and server.
