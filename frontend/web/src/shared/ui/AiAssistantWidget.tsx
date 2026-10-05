/* eslint-disable react-hooks/set-state-in-effect */
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useLocation } from "react-router-dom";
import { useAiAssistant } from "../ai/useAiAssistant";

const WINDOW_MIN_W = 280;
const WINDOW_MAX_W = 520;
const WINDOW_MIN_H = 240;
const WINDOW_MAX_H = 520;
const WINDOW_PADDING = 12;
const BUTTON_FALLBACK_W = 140;
const BUTTON_FALLBACK_H = 44;
const AI_WIDGET_STORAGE_KEYS = {
	width: "ai_widget:v2:width",
	height: "ai_widget:v2:height",
	windowPosition: "ai_widget:v2:window-position",
	buttonPosition: "ai_widget:v2:button-position",
} as const;
const AI_WIDGET_LEGACY_KEYS = {
	width: "ai_widget_width",
	height: "ai_widget_height",
	windowPosition: "ai_widget_window_pos",
	buttonPosition: "ai_widget_button_pos",
} as const;

type Point = { x: number; y: number };
type ResizeDir = "left" | "right" | "top" | "bottom" | "top-left" | "top-right" | "bottom-left" | "bottom-right";
type DragState = {
	type: "none" | "button" | "window" | "resize";
	dir?: ResizeDir;
	startX: number;
	startY: number;
	startW: number;
	startH: number;
	startPos: Point;
	offset?: Point;
	moved: boolean;
};

function formatTime(ts: number) {
	return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function clamp(value: number, min: number, max: number) {
	return Math.min(Math.max(value, min), max);
}

function clampWindowPosition(pos: Point, size: { w: number; h: number }): Point {
	const maxX = Math.max(WINDOW_PADDING, window.innerWidth - size.w - WINDOW_PADDING);
	const maxY = Math.max(WINDOW_PADDING, window.innerHeight - size.h - WINDOW_PADDING);
	return {
		x: clamp(pos.x, WINDOW_PADDING, maxX),
		y: clamp(pos.y, WINDOW_PADDING, maxY),
	};
}

function clampButtonPosition(pos: Point, size: { w: number; h: number }): Point {
	const maxX = Math.max(0, window.innerWidth - size.w);
	const minY = window.innerWidth < 768
		? (document.querySelector<HTMLElement>("header[data-app-header]")?.offsetHeight ?? 0) + WINDOW_PADDING
		: 0;
	const maxY = Math.max(minY, window.innerHeight - size.h);
	return {
		x: clamp(pos.x, 0, maxX),
		y: clamp(pos.y, minY, maxY),
	};
}

export function AiAssistantWidget() {
	const location = useLocation();
	const [input, setInput] = useState("");
	const [width, setWidth] = useState(360);
	const [height, setHeight] = useState(320);
	const [windowPosition, setWindowPosition] = useState<Point | null>(null);
	const [buttonPosition, setButtonPosition] = useState<Point | null>(null);
	const [animated, setAnimated] = useState<Record<number, string>>({});
	const prevOpenRef = useRef(false);

	const scrollRef = useRef<HTMLDivElement | null>(null);
	const stickToBottomRef = useRef(true);
	const buttonRef = useRef<HTMLButtonElement | null>(null);
	const animatedRef = useRef<Record<number, string>>({});
	const typingTimerRef = useRef<number | null>(null);
	const [welcomeTs] = useState(() => Date.now());
	const getButtonSize = () => {
		const rect = buttonRef.current?.getBoundingClientRect();
		return {
			w: rect?.width ?? BUTTON_FALLBACK_W,
			h: rect?.height ?? BUTTON_FALLBACK_H,
		};
	};
	const dragRef = useRef<DragState>({
		type: "none",
		startX: 0,
		startY: 0,
		startW: 0,
		startH: 0,
		startPos: { x: 0, y: 0 },
		moved: false,
	});

	const context = `Page: ${location.pathname}`;
	const assistant = useAiAssistant({
		context,
	});
	const layoutRef = useRef({
		width,
		height,
		windowPosition,
		buttonPosition,
		isOpen: assistant.isOpen,
	});

	useEffect(() => {
		layoutRef.current = {
			width,
			height,
			windowPosition,
			buttonPosition,
			isOpen: assistant.isOpen,
		};
	}, [assistant.isOpen, buttonPosition, height, width, windowPosition]);

	useEffect(() => {
		try {
			const storedWidth = Number(
				localStorage.getItem(AI_WIDGET_STORAGE_KEYS.width) ??
					localStorage.getItem(AI_WIDGET_LEGACY_KEYS.width) ??
					360,
			);
			const storedHeight = Number(
				localStorage.getItem(AI_WIDGET_STORAGE_KEYS.height) ??
					localStorage.getItem(AI_WIDGET_LEGACY_KEYS.height) ??
					320,
			);
			if (Number.isFinite(storedWidth)) setWidth(clamp(storedWidth, WINDOW_MIN_W, WINDOW_MAX_W));
			if (Number.isFinite(storedHeight)) setHeight(clamp(storedHeight, WINDOW_MIN_H, WINDOW_MAX_H));

			const rawWindowPos =
				localStorage.getItem(AI_WIDGET_STORAGE_KEYS.windowPosition) ??
				localStorage.getItem(AI_WIDGET_LEGACY_KEYS.windowPosition);
			if (rawWindowPos) {
				const parsed = JSON.parse(rawWindowPos) as Point;
				if (Number.isFinite(parsed.x) && Number.isFinite(parsed.y)) {
					setWindowPosition(parsed);
				}
			}

			const rawButtonPos =
				localStorage.getItem(AI_WIDGET_STORAGE_KEYS.buttonPosition) ??
				localStorage.getItem(AI_WIDGET_LEGACY_KEYS.buttonPosition);
			if (rawButtonPos) {
				const parsed = JSON.parse(rawButtonPos) as Point;
				if (Number.isFinite(parsed.x) && Number.isFinite(parsed.y)) {
					setButtonPosition(parsed);
				}
			}
		} catch {
			// Storage may be unavailable or contain an older invalid value.
		}
	}, []);

	useEffect(() => {
		const timer = window.setTimeout(() => {
			try {
				localStorage.setItem(AI_WIDGET_STORAGE_KEYS.width, String(width));
				localStorage.setItem(AI_WIDGET_STORAGE_KEYS.height, String(height));
				if (windowPosition) {
					localStorage.setItem(
						AI_WIDGET_STORAGE_KEYS.windowPosition,
						JSON.stringify(windowPosition),
					);
				}
				if (buttonPosition) {
					localStorage.setItem(
						AI_WIDGET_STORAGE_KEYS.buttonPosition,
						JSON.stringify(buttonPosition),
					);
				}
			} catch {
				// Ignore private mode, quota and disabled-storage failures.
			}
		}, 250);
		return () => window.clearTimeout(timer);
	}, [buttonPosition, height, width, windowPosition]);

	useEffect(() => {
		if (!windowPosition) {
			const fallback = clampWindowPosition(
				{ x: window.innerWidth - width - 24, y: 88 },
				{ w: width, h: height }
			);
			setWindowPosition(fallback);
			return;
		}
		const next = clampWindowPosition(windowPosition, { w: width, h: height });
		if (next.x !== windowPosition.x || next.y !== windowPosition.y) {
			setWindowPosition(next);
		}
	}, [windowPosition, width, height]);

	useEffect(() => {
		if (!buttonPosition) {
			const fallback = clampButtonPosition(
				{ x: window.innerWidth - BUTTON_FALLBACK_W - 24, y: 88 },
				{ w: BUTTON_FALLBACK_W, h: BUTTON_FALLBACK_H }
			);
			setButtonPosition(fallback);
			return;
		}
		const rect = buttonRef.current?.getBoundingClientRect();
		const next = clampButtonPosition(buttonPosition, {
			w: rect?.width ?? BUTTON_FALLBACK_W,
			h: rect?.height ?? BUTTON_FALLBACK_H,
		});
		if (next.x !== buttonPosition.x || next.y !== buttonPosition.y) {
			setButtonPosition(next);
		}
	}, [buttonPosition]);

	useEffect(() => {
		const wasOpen = prevOpenRef.current;
		prevOpenRef.current = assistant.isOpen;
		if (assistant.isOpen && !wasOpen && buttonPosition) {
			const { w: btnW, h: btnH } = getButtonSize();
			let nextX = buttonPosition.x;
			let nextY = buttonPosition.y;
			if (nextX + width + WINDOW_PADDING > window.innerWidth) {
				nextX = buttonPosition.x + btnW - width;
			}
			if (nextY + height + WINDOW_PADDING > window.innerHeight) {
				nextY = buttonPosition.y - height - 8;
			} else {
				nextY = buttonPosition.y + btnH + 8;
			}
			const next = clampWindowPosition({ x: nextX, y: nextY }, { w: width, h: height });
			setWindowPosition(next);
		}
	}, [assistant.isOpen, buttonPosition, width, height]);

	useEffect(() => {
		animatedRef.current = animated;
	}, [animated]);

	useEffect(() => {
		let moveFrame: number | null = null;
		let pendingMove: PointerEvent | null = null;

		const applyMove = (event: PointerEvent) => {
			if (dragRef.current.type === "none") return;
			const buttonSize = getButtonSize();
			const layout = layoutRef.current;

			if (dragRef.current.type === "resize") {
				const dx = event.clientX - dragRef.current.startX;
				const dy = event.clientY - dragRef.current.startY;
				let nextW = dragRef.current.startW;
				let nextH = dragRef.current.startH;
				let nextX = dragRef.current.startPos.x;
				let nextY = dragRef.current.startPos.y;

				if (dragRef.current.dir === "right" || dragRef.current.dir === "bottom-right" || dragRef.current.dir === "top-right") {
					nextW = clamp(dragRef.current.startW + dx, WINDOW_MIN_W, WINDOW_MAX_W);
				}
				if (dragRef.current.dir === "bottom" || dragRef.current.dir === "bottom-right" || dragRef.current.dir === "bottom-left") {
					nextH = clamp(dragRef.current.startH + dy, WINDOW_MIN_H, WINDOW_MAX_H);
				}
				if (dragRef.current.dir === "left" || dragRef.current.dir === "top-left" || dragRef.current.dir === "bottom-left") {
					const w = clamp(dragRef.current.startW - dx, WINDOW_MIN_W, WINDOW_MAX_W);
					nextX = dragRef.current.startPos.x + (dragRef.current.startW - w);
					nextW = w;
				}
				if (dragRef.current.dir === "top" || dragRef.current.dir === "top-left" || dragRef.current.dir === "top-right") {
					const h = clamp(dragRef.current.startH - dy, WINDOW_MIN_H, WINDOW_MAX_H);
					nextY = dragRef.current.startPos.y + (dragRef.current.startH - h);
					nextH = h;
				}

				setWidth(nextW);
				setHeight(nextH);
				if (layout.isOpen) {
					const next = clampWindowPosition({ x: nextX, y: nextY }, { w: nextW, h: nextH });
					setWindowPosition(next);
					setButtonPosition(clampButtonPosition(next, buttonSize));
				}
				return;
			}

			if (dragRef.current.type === "window") {
				const offset = dragRef.current.offset ?? { x: 0, y: 0 };
				const next = clampWindowPosition(
					{
						x: event.clientX - offset.x,
						y: event.clientY - offset.y,
					},
					{ w: layout.width, h: layout.height }
				);
				setWindowPosition(next);
				setButtonPosition(clampButtonPosition(next, buttonSize));
				return;
			}

			if (dragRef.current.type === "button") {
				const next = clampButtonPosition(
					{
						x: event.clientX - dragRef.current.startX + dragRef.current.startPos.x,
						y: event.clientY - dragRef.current.startY + dragRef.current.startPos.y,
					},
					buttonSize
				);
				const dx = event.clientX - dragRef.current.startX;
				const dy = event.clientY - dragRef.current.startY;
				if (Math.hypot(dx, dy) > 4) dragRef.current.moved = true;
				setButtonPosition(next);
			}
		};

		const handleMove = (event: PointerEvent) => {
			pendingMove = event;
			if (moveFrame !== null) return;
			moveFrame = window.requestAnimationFrame(() => {
				moveFrame = null;
				const nextMove = pendingMove;
				pendingMove = null;
				if (nextMove) applyMove(nextMove);
			});
		};

		const handleUp = () => {
			if (dragRef.current.type !== "none") {
				dragRef.current.type = "none";
				document.body.style.userSelect = "";
			}
		};

		window.addEventListener("pointermove", handleMove);
		window.addEventListener("pointerup", handleUp);
		return () => {
			if (moveFrame !== null) window.cancelAnimationFrame(moveFrame);
			window.removeEventListener("pointermove", handleMove);
			window.removeEventListener("pointerup", handleUp);
		};
	}, []);

	useEffect(() => {
		const lastAssistant = [...assistant.messages].reverse().find((msg) => msg.role === "assistant");
		if (!lastAssistant) return;
		if (animatedRef.current[lastAssistant.ts] === lastAssistant.content) return;
		if (typingTimerRef.current) {
			window.clearInterval(typingTimerRef.current);
			typingTimerRef.current = null;
		}
		const words = lastAssistant.content.split(/\s+/).filter(Boolean);
		if (words.length === 0) {
			setAnimated((prev) => ({ ...prev, [lastAssistant.ts]: lastAssistant.content }));
			return;
		}
		let index = 0;
		setAnimated((prev) => ({ ...prev, [lastAssistant.ts]: "" }));
		typingTimerRef.current = window.setInterval(() => {
			index += 1;
			setAnimated((prev) => ({
				...prev,
				[lastAssistant.ts]: words.slice(0, index).join(" "),
			}));
			if (index >= words.length) {
				if (typingTimerRef.current) {
					window.clearInterval(typingTimerRef.current);
					typingTimerRef.current = null;
				}
				setAnimated((prev) => ({ ...prev, [lastAssistant.ts]: lastAssistant.content }));
			}
		}, 40);
		return () => {
			if (typingTimerRef.current) {
				window.clearInterval(typingTimerRef.current);
				typingTimerRef.current = null;
			}
		};
	}, [assistant.messages]);

	useEffect(() => {
		const container = scrollRef.current;
		if (!container) return;
		const handleScroll = () => {
			const distance = container.scrollHeight - container.scrollTop - container.clientHeight;
			stickToBottomRef.current = distance < 80;
		};
		container.addEventListener("scroll", handleScroll);
		return () => container.removeEventListener("scroll", handleScroll);
	}, []);

	useEffect(() => {
		if (!scrollRef.current || !stickToBottomRef.current) return;
		scrollRef.current.scrollTo({
			top: scrollRef.current.scrollHeight,
			behavior: "smooth",
		});
	}, [assistant.messages, animated, assistant.isSending]);

	async function handleSend() {
		if (!input.trim() || assistant.isSending) return;
		const prompt = input.trim();
		stickToBottomRef.current = true;
		void assistant.send(prompt);
		setInput("");
	}

	const showWelcome = assistant.isOpen && assistant.messages.length === 0;

	const renderMessage = (msg: { role: "user" | "assistant"; content: string; ts: number }) => {
		const isAssistant = msg.role === "assistant";
		const display = isAssistant && animated[msg.ts] !== undefined ? animated[msg.ts] : msg.content;
		return (
			<div key={msg.ts} className={"flex flex-col gap-1 " + (msg.role === "user" ? "items-end" : "items-start")}>
				<div
					className={
						"max-w-[85%] rounded-2xl px-3 py-2 text-sm transition-opacity duration-200 " +
						(msg.role === "user"
							? "bg-accent text-accent-foreground border border-border"
							: "bg-secondary text-secondary-foreground border border-border")
					}
				>
					{display}
					{isAssistant && animated[msg.ts] !== undefined && display !== msg.content && (
						<span className="ml-1 inline-block h-3 w-[2px] animate-[typing-caret_800ms_step-end_infinite] bg-muted-foreground align-middle" />
					)}
				</div>
				<div className="text-[10px] text-muted-foreground">{formatTime(msg.ts)}</div>
			</div>
		);
	};

	const containerStyle = assistant.isOpen
		? windowPosition
			? { left: windowPosition.x, top: windowPosition.y }
			: undefined
		: buttonPosition
			? { left: buttonPosition.x, top: buttonPosition.y }
			: undefined;

	const widget = (
		<div
			className={`fixed flex flex-col items-end gap-3 ${assistant.isOpen ? "z-[350]" : "z-[65]"}`}
			style={containerStyle}
		>
			{assistant.isOpen && (
				<div
					className="rounded-2xl border border-border bg-popover text-popover-foreground shadow-lg animate-[assistant-in_200ms_ease-out] flex flex-col"
					style={{ width, height }}
				>
					<div
						className="flex items-center justify-between border-b border-border px-4 py-3 cursor-move select-none"
						onPointerDown={(event) => {
							event.preventDefault();
							event.stopPropagation();
							if (event.currentTarget instanceof HTMLElement) {
								event.currentTarget.setPointerCapture(event.pointerId);
							}
							document.body.style.userSelect = "none";
							dragRef.current = {
								type: "window",
								startX: event.clientX,
								startY: event.clientY,
								startW: width,
								startH: height,
								startPos: windowPosition ?? { x: 0, y: 0 },
								offset: {
									x: event.clientX - (windowPosition?.x ?? 0),
									y: event.clientY - (windowPosition?.y ?? 0),
								},
								moved: false,
							};
						}}
					>
						<div>
							<div className="text-sm font-semibold text-foreground">AI помощник</div>
							<div className="text-xs text-muted-foreground">OpenAI · GPT-5.4 mini</div>
						</div>
						<div className="flex items-center gap-2">
							<button
									onPointerDown={(event) => {
										event.stopPropagation();
									}}
								onClick={() => assistant.setIsOpen(false)}
								className="rounded-lg border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
							>
								Скрыть
							</button>
						</div>
					</div>

					<div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-4 py-3 text-sm">
						{assistant.error && (
							<div className="rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
								{assistant.error}
							</div>
						)}
						{showWelcome && (
							<div className="flex flex-col gap-1 items-start">
								<div className="max-w-[85%] rounded-2xl px-3 py-2 text-sm bg-secondary text-secondary-foreground border border-border">
									Привет! Я бот‑помощник Short&Long. Спроси меня о сигналах, настройках, стратегиях или любой инфе по платформе — отвечу кратко и по делу.
								</div>
								<div className="text-[10px] text-muted-foreground">{welcomeTs ? formatTime(welcomeTs) : ""}</div>
							</div>
						)}
						{assistant.messages.map(renderMessage)}
						{assistant.isSending && (
							<div className="flex items-start">
								<div className="rounded-2xl border border-border bg-secondary px-3 py-2 text-sm text-muted-foreground">
									Думаю
									<span className="ml-1 inline-flex gap-1">
										<span className="dot" />
										<span className="dot" />
										<span className="dot" />
									</span>
								</div>
							</div>
						)}
					</div>

					<div className="border-t border-border px-4 py-3">
						<div className="flex items-center gap-2">
							<input
								value={input}
								maxLength={1600}
								onChange={(e) => setInput(e.target.value)}
								onKeyDown={(e) => {
									if (e.key === "Enter") handleSend();
								}}
								placeholder="Спроси о сделках, сигналах, настройках..."
								className="w-full rounded-xl border border-input bg-background px-3 py-2 text-sm text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30"
							/>
							<button
								onClick={handleSend}
								disabled={assistant.isSending}
								className="rounded-xl bg-primary px-3 py-2 text-xs font-semibold text-primary-foreground hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50"
							>
								{assistant.isSending ? "..." : "Отправить"}
							</button>
						</div>
					</div>

					{([
						{ dir: "left", cls: "absolute left-0 top-0 h-full w-2 cursor-ew-resize" },
						{ dir: "right", cls: "absolute right-0 top-0 h-full w-2 cursor-ew-resize" },
						{ dir: "top", cls: "absolute top-0 left-0 h-2 w-full cursor-ns-resize" },
						{ dir: "bottom", cls: "absolute bottom-0 left-0 h-2 w-full cursor-ns-resize" },
						{ dir: "top-left", cls: "absolute left-0 top-0 h-3 w-3 cursor-nwse-resize" },
						{ dir: "top-right", cls: "absolute right-0 top-0 h-3 w-3 cursor-nesw-resize" },
						{ dir: "bottom-left", cls: "absolute left-0 bottom-0 h-3 w-3 cursor-nesw-resize" },
						{ dir: "bottom-right", cls: "absolute right-0 bottom-0 h-3 w-3 cursor-nwse-resize" },
					] as { dir: ResizeDir; cls: string }[]).map((handle) => (
						<div
							key={handle.dir}
							className={handle.cls}
							onPointerDown={(event) => {
								event.preventDefault();
								event.stopPropagation();
								document.body.style.userSelect = "none";
								dragRef.current = {
									type: "resize",
									dir: handle.dir,
									startX: event.clientX,
									startY: event.clientY,
									startW: width,
									startH: height,
									startPos: windowPosition ?? { x: 0, y: 0 },
									moved: false,
								};
							}}
						/>
					))}
				</div>
			)}

			{!assistant.isOpen && (
				<button
					ref={buttonRef}
					onClick={() => {
						if (dragRef.current.moved) {
							dragRef.current.moved = false;
							return;
						}
						assistant.setIsOpen(true);
					}}
					onPointerDown={(event) => {
						event.preventDefault();
						event.stopPropagation();
						document.body.style.userSelect = "none";
						dragRef.current = {
							type: "button",
							startX: event.clientX,
							startY: event.clientY,
							startW: width,
							startH: height,
							startPos: buttonPosition ?? { x: 0, y: 0 },
							moved: false,
						};
					}}
					className="rounded-full border border-border bg-card px-4 py-2 text-sm font-semibold text-card-foreground transition hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
				>
					AI помощник
				</button>
			)}

			<style>{`
				@keyframes assistant-in {
					from { opacity: 0; transform: translateY(8px) scale(0.98); }
					to { opacity: 1; transform: translateY(0) scale(1); }
				}
				@keyframes typing-caret {
					0%, 100% { opacity: 0; }
					50% { opacity: 1; }
				}
				.dot {
					width: 4px;
					height: 4px;
					border-radius: 999px;
					background: var(--muted-foreground);
					display: inline-block;
					animation: dot-pulse 1s infinite ease-in-out;
				}
				.dot:nth-child(2) { animation-delay: 150ms; }
				.dot:nth-child(3) { animation-delay: 300ms; }
				@keyframes dot-pulse {
					0%, 80%, 100% { opacity: 0.25; transform: translateY(0); }
					40% { opacity: 1; transform: translateY(-2px); }
				}
			`}</style>
		</div>
	);

	return typeof document === "undefined" ? null : createPortal(widget, document.body);
}
