import { http } from "../api/http";

export type AiMessage = {
  role: "user" | "assistant";
  content: string;
};

export type AiChatRequest = {
  messages: AiMessage[];
  context?: string;
};

export type AiChatResponse = {
  reply: string;
  model: string;
};

export async function aiChat(input: AiChatRequest, signal?: AbortSignal): Promise<AiChatResponse> {
  return http<AiChatResponse>("/api/ai/chat", {
    method: "POST",
    body: JSON.stringify(input),
    signal,
  });
}
