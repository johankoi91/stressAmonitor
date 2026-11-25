export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch("/api" + path, {
    method,
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  });
  const value = await response.json();
  if (!response.ok)
    throw new APIError(value.error || "请求失败", response.status);
  return value as T;
}

export function uploadAudio(
  file: File,
  sampleRate: number,
  channels: number,
  progress: (value: number) => void,
): Promise<import("./types").AudioFile> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append("file", file);
    form.append("sampleRate", String(sampleRate));
    form.append("channels", String(channels));
    const request = new XMLHttpRequest();
    request.open("POST", "/api/audio");
    request.responseType = "json";
    request.upload.onprogress = (event) => {
      if (event.lengthComputable)
        progress(Math.round((event.loaded * 100) / event.total));
    };
    request.onload = () => {
      if (request.status >= 200 && request.status < 300)
        resolve(request.response);
      else
        reject(
          new APIError(
            request.response?.error || "音频上传失败",
            request.status,
          ),
        );
    };
    request.onerror = () =>
      reject(new Error("音频上传失败，请检查与中心的连接"));
    request.send(form);
  });
}
