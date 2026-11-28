// Display-only normalization. Keep the original API data and send body intact.
export function boardText(value: string): string {
  return value.replace(/\r\n/g, '\n').replace(/\t/g, ' ').replace(
    /[\u0000-\u0009\u000b-\u001f\u007f-\u009f\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g,
    character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`,
  )
}
