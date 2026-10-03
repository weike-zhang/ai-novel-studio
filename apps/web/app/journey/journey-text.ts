const maxParagraphChars = 96;
const maxSentencesPerParagraph = 5;

function splitLongParagraph(value: string): string[] {
  if (value.length <= maxParagraphChars) return [value];

  const sentences = value.match(/[^。！？!?]+[。！？!?](?:[”」』）】])?/gu) ?? [];
  if (sentences.length === 0) {
    const chunks: string[] = [];
    for (let index = 0; index < value.length; index += maxParagraphChars) {
      chunks.push(value.slice(index, index + maxParagraphChars));
    }
    return chunks;
  }

  const paragraphs: string[] = [];
  let current = "";
  let sentenceCount = 0;
  for (const sentence of sentences) {
    if (current && (current.length + sentence.length > maxParagraphChars || sentenceCount >= maxSentencesPerParagraph)) {
      paragraphs.push(current);
      current = "";
      sentenceCount = 0;
    }
    current += sentence;
    sentenceCount += 1;
  }
  if (current) paragraphs.push(current);

  const matchedLength = sentences.join("").length;
  if (matchedLength < value.length) {
    const remainder = value.slice(matchedLength).trim();
    if (remainder) paragraphs.push(...splitLongParagraph(remainder));
  }
  return paragraphs;
}

export function splitStoryParagraphs(value: string): string[] {
  return value
    .replace(/\r/g, "")
    .trim()
    .split(/\n+/u)
    .map((paragraph) => paragraph.trim())
    .filter(Boolean)
    .flatMap(splitLongParagraph);
}
