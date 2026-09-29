import '@testing-library/jest-dom'

// xterm.js measures glyphs through a 2d canvas context when its module is imported.
// jsdom does not implement one, so stub the minimum it needs to stay quiet. Tests that
// mock xterm.js entirely are unaffected.
HTMLCanvasElement.prototype.getContext = (() => ({
  measureText: () => ({ width: 8 }),
})) as unknown as HTMLCanvasElement['getContext']
