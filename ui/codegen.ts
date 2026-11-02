import type { CodegenConfig } from '@graphql-codegen/cli'

// Reads the committed SDL snapshot (gen/schema.graphql, from `para schema dump`), so codegen
// needs no running server. Output lands in src/gql/ (gitignored; every script runs `pnpm gen`).
const config: CodegenConfig = {
  overwrite: true,
  schema: 'gen/schema.graphql',
  documents: ['src/**/*.{ts,tsx}'],
  generates: {
    './src/gql/': {
      preset: 'client',
      // Fragments are plain selections here: no useFragment() unwrapping.
      presetConfig: { fragmentMasking: false },
      config: {
        useTypeImports: true,
        // Documents are query strings typed by phantom generics: no parsed AST in the bundle.
        documentMode: 'string',
        // gat emits Long as a JSON string. Para has no Long field today; keep the rule.
        scalars: { Long: { input: 'string', output: 'string' } },
        // Enums are string-literal unions.
        enumsAsTypes: true,
      },
    },
  },
  ignoreNoDocuments: true,
}

export default config
