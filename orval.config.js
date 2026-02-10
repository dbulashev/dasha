module.exports = {
    api: {
        output: {
            mode: 'tags-split',
            target: './frontend/src/api/gen/generated.ts',
            client: 'vue-query',
            clean: true,
            prettier: true,
            schemas: './frontend/src/api/models',
            mock: false
        },
        input: {
            target: './doc/swagger.yaml'
        }
    }
}