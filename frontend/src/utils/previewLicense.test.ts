import { expect, it } from 'vitest'
import { previewLicenseNotice } from './previewLicense'

it('retains the complete license files when they exist', () => {
  expect(previewLicenseNotice({ name: 'example', version: '1.0', license: 'MIT' }, ['Copyright Example\nTerms']))
    .toBe('example@1.0\nCopyright Example\nTerms')
})

it('includes the declared license when a contributing package has no notice file', () => {
  expect(previewLicenseNotice({ name: 'fastdom', version: '1.0.12', license: 'MIT' }, []))
    .toBe('fastdom@1.0.12\nDeclared license: MIT')
})

it('fails instead of silently omitting a package with no license information', () => {
  expect(() => previewLicenseNotice({ name: 'missing', version: '1.0' }, []))
    .toThrow('missing@1.0 has no license notice or declared license')
})
