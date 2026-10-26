import fs from 'node:fs'
import path from 'node:path'
import ts from 'typescript'
import { parse, compileTemplate } from '@vue/compiler-sfc'

const root = path.resolve(process.argv[2] || '../..')
const prefix = 'web/src/wsl/'
const configPath = path.join(root, 'web/tsconfig.json')
const { config, error } = ts.readConfigFile(configPath, ts.sys.readFile)
if (error) throw new Error('Cannot parse frontend tsconfig')
const paths = config.compilerOptions?.paths || {}
const files = []
function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const name = path.join(dir, entry.name)
    if (entry.isSymbolicLink()) throw new Error('Source symlink is not permitted')
    if (entry.isDirectory()) walk(name)
    else if (/\.(ts|vue)$/.test(name)) files.push(name)
  }
}
walk(path.join(root, prefix))
const nodes = files.map(file => ({ id: path.relative(root, file).split(path.sep).join('/'), kind: 'frontend' }))
const edges = []
const gaps = []
function resolve(from, specifier) {
  let base
  if (specifier.startsWith('.')) base = path.resolve(path.dirname(from), specifier)
  else {
    for (const [pattern, targets] of Object.entries(paths)) {
      const [start, end = ''] = pattern.split('*')
      if (specifier.startsWith(start) && specifier.endsWith(end)) {
        const part = specifier.slice(start.length, end ? -end.length : undefined)
        base = path.resolve(root, 'web', targets[0].replace('*', part))
        break
      }
    }
  }
  if (!base) return { id: `npm:${specifier}`, external: true }
  const resolved = [base, `${base}.ts`, `${base}.vue`, path.join(base, 'index.ts')]
    .find(p => fs.existsSync(p) && fs.statSync(p).isFile())
  if (!resolved || !resolved.startsWith(root + path.sep)) {
    gaps.push({ file: path.relative(root, from), kind: 'unresolved-import', specifier })
    return null
  }
  const relative = path.relative(root, resolved).split(path.sep).join('/')
  return { id: relative, external: !relative.startsWith(prefix) }
}
for (const file of files.sort()) {
  const source = fs.readFileSync(file, 'utf8')
  const relative = path.relative(root, file).split(path.sep).join('/')
  let scripts = [{ content: source, line: 1 }]
  let template = ''
  if (file.endsWith('.vue')) {
    const parsed = parse(source, { filename: file })
    if (parsed.errors.length) throw new Error(`Vue parse failed: ${relative}`)
    scripts = [parsed.descriptor.script, parsed.descriptor.scriptSetup].filter(Boolean)
      .map(s => ({ content: s.content, line: s.loc.start.line }))
    template = parsed.descriptor.template?.content || ''
    if (template) {
      const compiled = compileTemplate({ source: template, filename: file, id: relative })
      if (compiled.errors.length) throw new Error(`Vue template parse failed: ${relative}`)
    }
  }
  for (const script of scripts) {
    const ast = ts.createSourceFile(file + '.ts', script.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
    if (ast.parseDiagnostics.length) throw new Error(`TypeScript parse failed: ${relative}`)
    const add = (expression, kind, bindings = []) => {
      const to = resolve(file, expression.text)
      if (!to) return
      const line = ast.getLineAndCharacterOfPosition(expression.getStart(ast)).line + script.line
      edges.push({ from: relative, to: to.id, kind, external: to.external,
        evidence: [{ path: relative, line, symbol: expression.text }],
        template_used: bindings.some(name => {
          const kebab = name.replace(/[A-Z]/g, (m, index) => (index ? '-' : '') + m.toLowerCase())
          return new RegExp(`<(${name}|${kebab})([\\s/>])`).test(template)
        }) })
    }
    const visit = node => {
      if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
        const bindings = []
        if (node.importClause?.name) bindings.push(node.importClause.name.text)
        const named = node.importClause?.namedBindings
        if (named && ts.isNamedImports(named)) bindings.push(...named.elements.map(e => e.name.text))
        add(node.moduleSpecifier, node.importClause?.isTypeOnly ? 'type-import' : 'import', bindings)
      } else if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
        add(node.moduleSpecifier, 're-export')
      } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
        if (node.arguments.length === 1 && ts.isStringLiteral(node.arguments[0])) add(node.arguments[0], 'dynamic-import')
        else gaps.push({ file: relative, kind: 'nonliteral-dynamic-import' })
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  if (/<component[\s>]/.test(template)) gaps.push({ file: relative, kind: 'runtime-component-selection' })
}
console.log(JSON.stringify({ nodes, edges, gaps }))
