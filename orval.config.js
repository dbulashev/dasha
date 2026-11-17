module.exports = {
    api: {
        output: {
            mode: 'tags-split',
            target: './frontend/src/api/gen/generated.ts',
            client: 'vue-query',
            clean: true,
            prettier: true,
            schemas: './frontend/src/api/models',
            mock: false,
            override: {
                mutator: {
                    path: './frontend/src/api/customFetch.ts',
                    name: 'customFetch'
                }
            }
        },
        input: {
            target: './doc/swagger.yaml',
            // The PII catalog is for an external classifier with a static token;
            // the UI must never be able to call it.
            filters: {
                mode: 'exclude',
                tags: ['pii-catalog'],
                schemas: [/^PiiCatalog/]
            }
        }
    }
}