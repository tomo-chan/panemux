interface PackageLicense {
  name: string
  version: string
  license?: string
}

// A contributing package must have either distributed notices or an explicit
// license declaration. Missing information must fail the build visibly.
export function previewLicenseNotice(pkg: PackageLicense, files: string[]): string {
  const name = `${pkg.name}@${pkg.version}`
  if (files.length > 0) return `${name}\n${files.join('\n\n')}`
  if (typeof pkg.license === 'string' && pkg.license.trim()) {
    return `${name}\nDeclared license: ${pkg.license}`
  }
  throw new Error(`${name} has no license notice or declared license`)
}
