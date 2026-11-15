import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const js = require('@eslint/js')
const tseslint = require('typescript-eslint')
const vue = require('eslint-plugin-vue')
const globals = require('globals')
const ts = require('typescript')
const { parse } = require('@vue/compiler-sfc')
const web = path.dirname(fileURLToPath(import.meta.url))
const source = path.join(web, 'src')
const scriptExtensions = ['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.vue']
const textExtensions = [...scriptExtensions, '.json', '.css', '.scss', '.less', '.html', '.md']
const config = ts.readConfigFile(path.join(web, 'tsconfig.json'), ts.sys.readFile)
if (config.error) throw new Error(ts.flattenDiagnosticMessageText(config.error.messageText, '\n'))
const { options } = ts.parseJsonConfigFileContent(config.config, ts.sys, web)

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const filename = path.join(directory, entry.name)
    if (entry.isSymbolicLink()) throw new Error(`Product source symlink: ${filename}`)
    return entry.isDirectory() ? walk(filename) : [filename]
  })
}

function imports(code, filename) {
  const result = []
  const tree = ts.createSourceFile(filename, code, ts.ScriptTarget.Latest, true)
  function visit(node) {
    if (
      (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) &&
      node.moduleSpecifier &&
      ts.isStringLiteral(node.moduleSpecifier)
    ) {
      result.push(node.moduleSpecifier.text)
    } else if (
      ts.isImportTypeNode(node) &&
      ts.isLiteralTypeNode(node.argument) &&
      ts.isStringLiteral(node.argument.literal)
    ) {
      result.push(node.argument.literal.text)
    } else if (
      ts.isCallExpression(node) &&
      (node.expression.kind === ts.SyntaxKind.ImportKeyword ||
        (ts.isIdentifier(node.expression) && node.expression.text === 'require')) &&
      node.arguments.length === 1 &&
      ts.isStringLiteral(node.arguments[0])
    ) {
      result.push(node.arguments[0].text)
    } else if (
      ts.isImportEqualsDeclaration(node) &&
      ts.isExternalModuleReference(node.moduleReference) &&
      node.moduleReference.expression &&
      ts.isStringLiteral(node.moduleReference.expression)
    ) {
      result.push(node.moduleReference.expression.text)
    }
    ts.forEachChild(node, visit)
  }
  visit(tree)
  return result
}

function styleImports(code) {
  return [...code.matchAll(/@(?:import|use|forward)\s+(?:url\(\s*)?['"]([^'"]+)['"]/g)].map(
    (match) => match[1]
  )
}

function resolveLocal(specifier, importer) {
  const clean = specifier.split(/[?#]/, 1)[0]
  const aliases = Object.entries(options.paths || {}).sort(
    ([left], [right]) => right.length - left.length
  )
  let bases = clean.startsWith('.') ? [path.resolve(path.dirname(importer), clean)] : []
  for (const [alias, targets] of aliases) {
    const prefix = alias.endsWith('*') ? alias.slice(0, -1) : alias
    if (clean === alias || (alias.endsWith('*') && clean.startsWith(prefix))) {
      bases = targets.map((target) =>
        path.resolve(options.baseUrl || web, target.replace('*', clean.slice(prefix.length)))
      )
      break
    }
  }
  if (!bases.length) return null
  const resolved = ts.resolveModuleName(clean, importer, options, ts.sys).resolvedModule
    ?.resolvedFileName
  const candidates = [
    ...(resolved ? [resolved] : []),
    ...bases.flatMap((base) => [
      base,
      ...textExtensions.map((extension) => base + extension),
      ...scriptExtensions.map((extension) => path.join(base, 'index' + extension))
    ])
  ]
  const filename = candidates.find(
    (candidate) => fs.existsSync(candidate) && fs.statSync(candidate).isFile()
  )
  if (!filename) throw new Error(`Unresolved product import ${specifier} in ${importer}`)
  const real = fs.realpathSync(filename)
  if (!real.startsWith(source + path.sep)) {
    throw new Error(`Product import outside src: ${specifier} in ${importer}`)
  }
  return real
}

// All product files are roots; only statically imported shared files join their scope.
const pending = [...walk(path.join(source, 'wsl')), path.join(source, 'env.d.ts')]
const visited = new Set()
while (pending.length) {
  const filename = pending.pop()
  if (visited.has(filename)) continue
  visited.add(filename)
  const extension = path.extname(filename)
  if (!textExtensions.includes(extension)) continue
  const code = fs.readFileSync(filename, 'utf8')
  let references = []
  if (extension === '.vue') {
    const { descriptor, errors } = parse(code, { filename })
    if (errors.length) throw new Error(`Invalid product SFC ${filename}: ${errors.join('; ')}`)
    for (const script of [descriptor.script, descriptor.scriptSetup].filter(Boolean)) {
      if (script.src) references.push(script.src)
      references.push(...imports(script.content, filename + '.ts'))
    }
    for (const style of descriptor.styles) {
      if (style.src) references.push(style.src)
      references.push(...styleImports(style.content))
    }
    if (descriptor.template?.src) references.push(descriptor.template.src)
  } else if (scriptExtensions.includes(extension)) {
    references = imports(code, filename)
  } else if (['.css', '.scss', '.less'].includes(extension)) {
    references = styleImports(code)
  }
  for (const reference of references) {
    const dependency = resolveLocal(reference, filename)
    if (dependency) pending.push(dependency)
  }
}

export const productFiles = [...visited]
  .filter((filename) => textExtensions.includes(path.extname(filename)))
  .map((filename) => path.relative(web, filename).split(path.sep).join('/'))
  .sort()
export const eslintFiles = productFiles.filter((filename) =>
  scriptExtensions.includes(path.extname(filename))
)
const vueFiles = eslintFiles.filter((filename) => filename.endsWith('.vue'))

export default [
  { ...js.configs.recommended, files: eslintFiles },
  ...tseslint.configs.recommended.map((preset) => ({ ...preset, files: eslintFiles })),
  ...vue.configs['flat/essential'].map((preset) => ({
    ...preset,
    files: preset.files ? vueFiles : eslintFiles
  })),
  {
    files: eslintFiles,
    linterOptions: { noInlineConfig: true, reportUnusedDisableDirectives: 'error' },
    languageOptions: { globals: { ...globals.browser } },
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      'vue/multi-word-component-names': 'off'
    }
  },
  {
    files: vueFiles,
    languageOptions: { parserOptions: { parser: tseslint.parser, extraFileExtensions: ['.vue'] } }
  }
]
