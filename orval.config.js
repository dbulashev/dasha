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
            target: './doc/swagger.yaml'
        }
    }
}