// Preserve the existing CI check name while running the replacement onboarding contract.
// Incoming onboarding keeps friend acceptance separate from private and group proofs.
// prettier-ignore
async (page) => {
  const source = process.getBuiltinModule('fs').readFileSync('tools/onboarding_incoming_ui_smoke.js', 'utf8')
  const run = process.getBuiltinModule('vm').runInThisContext(`(${source})`, {
    filename: 'onboarding_incoming_ui_smoke.js'
  })
  return run(page)
}
