const ACCEPTED_COVER_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;

export const COVER_UPLOAD_LIMITS = {
  maxRawBytes: 8 * 1024 * 1024,
  minWidth: 1280,
  minHeight: 720,
  outputWidth: 1280,
  outputHeight: 720,
  maxOutputBytes: 300 * 1024,
  accept: ACCEPTED_COVER_TYPES as readonly string[],
} as const;

export const COVER_UPLOAD_MESSAGES = {
  type: "请上传 JPG、PNG 或 WebP 图片。",
  size: `图片不能超过 ${COVER_UPLOAD_LIMITS.maxRawBytes / 1024 / 1024}MB。`,
  decode: "无法读取这张图片，请换一张再试。",
  clarity: `图片清晰度不足，宽高至少需要 ${COVER_UPLOAD_LIMITS.minWidth}×${COVER_UPLOAD_LIMITS.minHeight} 像素。`,
  encode: "图片处理失败，请换一张再试。",
} as const;

export function coverCropRect(
  sourceWidth: number,
  sourceHeight: number,
  targetWidth: number,
  targetHeight: number,
): { x: number; y: number; width: number; height: number } {
  const targetRatio = targetWidth / targetHeight;
  let width = sourceWidth;
  let height = Math.round(sourceWidth / targetRatio);
  if (height > sourceHeight) {
    height = sourceHeight;
    width = Math.round(sourceHeight * targetRatio);
  }
  return {
    x: Math.round((sourceWidth - width) / 2),
    y: Math.round((sourceHeight - height) / 2),
    width,
    height,
  };
}

export type CoverUploadResult =
  | { ok: true; blob: Blob; contentType: string }
  | { ok: false; message: string };

function loadImage(file: File): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const image = new Image();
    image.onload = () => {
      URL.revokeObjectURL(url);
      resolve(image);
    };
    image.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("image decode failed"));
    };
    image.src = url;
  });
}

function canvasToBlob(canvas: HTMLCanvasElement, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
}

async function encodeCover(canvas: HTMLCanvasElement): Promise<Blob | null> {
  const qualities = [0.85, 0.75, 0.6, 0.5];
  let last: Blob | null = null;
  for (const quality of qualities) {
    last = await canvasToBlob(canvas, quality);
    if (last && last.size <= COVER_UPLOAD_LIMITS.maxOutputBytes) return last;
  }
  return last;
}

export async function processCoverFile(file: File): Promise<CoverUploadResult> {
  if (!COVER_UPLOAD_LIMITS.accept.includes(file.type)) {
    return { ok: false, message: COVER_UPLOAD_MESSAGES.type };
  }
  if (file.size > COVER_UPLOAD_LIMITS.maxRawBytes) {
    return { ok: false, message: COVER_UPLOAD_MESSAGES.size };
  }
  let image: HTMLImageElement;
  try {
    image = await loadImage(file);
  } catch {
    return { ok: false, message: COVER_UPLOAD_MESSAGES.decode };
  }
  const { minWidth, minHeight } = COVER_UPLOAD_LIMITS;
  if (image.naturalWidth < minWidth || image.naturalHeight < minHeight) {
    return { ok: false, message: COVER_UPLOAD_MESSAGES.clarity };
  }
  const { outputWidth, outputHeight } = COVER_UPLOAD_LIMITS;
  const crop = coverCropRect(image.naturalWidth, image.naturalHeight, outputWidth, outputHeight);
  const canvas = document.createElement("canvas");
  canvas.width = outputWidth;
  canvas.height = outputHeight;
  const context = canvas.getContext("2d");
  if (!context) return { ok: false, message: COVER_UPLOAD_MESSAGES.encode };
  context.drawImage(image, crop.x, crop.y, crop.width, crop.height, 0, 0, outputWidth, outputHeight);
  const blob = await encodeCover(canvas);
  if (!blob) return { ok: false, message: COVER_UPLOAD_MESSAGES.encode };
  return { ok: true, blob, contentType: "image/jpeg" };
}
