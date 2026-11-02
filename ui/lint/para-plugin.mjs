// oxlint JS plugin: checks oxlint's native rules cannot express.
//  - no-window-fetch: the only network path is gqlClient. Bare `fetch` is banned by core
//    `no-restricted-globals` (.oxlintrc.json); `window.fetch` and `globalThis.fetch` are not global
//    references, so they land here. src/client/session.ts is the one file allowed to call fetch, and
//    only through the bare global.
const plugin = {
  meta: { name: 'para' },
  rules: {
    'no-window-fetch': {
      create(context) {
        return {
          MemberExpression(node) {
            const o = node.object
            if (
              o?.type === 'Identifier' &&
              (o.name === 'window' || o.name === 'globalThis' || o.name === 'self') &&
              ((!node.computed && node.property?.name === 'fetch') ||
                (node.computed && node.property?.value === 'fetch'))
            ) {
              context.report({
                node,
                message: 'Use the GraphQL client (gqlClient), not window.fetch.',
              })
            }
          },
        }
      },
    },
  },
}

export default plugin
