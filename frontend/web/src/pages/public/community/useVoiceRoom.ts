import { useCallback, useEffect, useRef, useState } from "react";
import {
  ConnectionState,
  DisconnectReason,
  Room,
  RoomEvent,
  Track,
  type Participant,
} from "livekit-client";
import { joinVoiceRoom, voiceError } from "./voiceApi";

export type VoiceMember = {
  identity: string;
  name: string;
  local: boolean;
  speaking: boolean;
  microphone: boolean;
  quality: string;
};

export function useVoiceRoom() {
  const roomRef = useRef<Room | null>(null);
  const epoch = useRef(0),
    request = useRef<AbortController | null>(null);
  const audioHostRef = useRef<HTMLDivElement | null>(null);
  const [pair, setPair] = useState<string | null>(null);
  const [state, setState] = useState<ConnectionState>(
    ConnectionState.Disconnected,
  );
  const [connecting, setConnecting] = useState(false),
    [busy, setBusy] = useState(false);
  const [members, setMembers] = useState<VoiceMember[]>([]);
  const [error, setError] = useState("");
  const [mic, setMic] = useState(false),
    [deaf, setDeaf] = useState(false);
  const [volume, setVolume] = useState(0.8),
    [audioBlocked, setAudioBlocked] = useState(false);
  const [input, setInput] = useState("default"),
    [output, setOutput] = useState("default");
  const audioPrefs = useRef({ volume, deaf });
  useEffect(() => {
    audioPrefs.current = { volume, deaf };
    for (const p of roomRef.current?.remoteParticipants.values() ?? [])
      p.setVolume(deaf ? 0 : volume);
  }, [volume, deaf]);

  const leave = useCallback(async () => {
    epoch.current++;
    request.current?.abort();
    request.current = null;
    const old = roomRef.current;
    roomRef.current = null;
    setConnecting(false);
    setBusy(false);
    setPair(null);
    setMembers([]);
    setMic(false);
    setDeaf(false);
    setState(ConnectionState.Disconnected);
    setAudioBlocked(false);
    audioHostRef.current?.replaceChildren();
    if (old) {
      old.removeAllListeners();
      await old.disconnect();
    }
  }, []);
  useEffect(
    () => () => {
      epoch.current++;
      request.current?.abort();
      const old = roomRef.current;
      roomRef.current = null;
      old?.removeAllListeners();
      void old?.disconnect();
    },
    [],
  );

  const join = useCallback(
    async (id: string) => {
      if (!window.isSecureContext) {
        setError(
          "Для голосового чата откройте сайт по HTTPS. По HTTP браузер не разрешает микрофон.",
        );
        return;
      }
      if (
        roomRef.current?.name === `pair-${id}` &&
        roomRef.current.state !== ConnectionState.Disconnected
      )
        return;
      const cleanup = leave();
      const generation = ++epoch.current;
      const controller = new AbortController();
      request.current = controller;
      setError("");
      setConnecting(true);
      let next: Room | null = null;
      try {
        await cleanup;
        if (epoch.current !== generation) return;
        const access = await joinVoiceRoom(id, controller.signal);
        if (epoch.current !== generation) return;
        next = new Room({
          adaptiveStream: false,
          dynacast: false,
          publishDefaults: { stopMicTrackOnMute: true },
          audioCaptureDefaults: {
            echoCancellation: true,
            noiseSuppression: true,
            autoGainControl: true,
            deviceId: input,
          },
          audioOutput: { deviceId: output },
        });
        const room = next;
        roomRef.current = room;
        const current = () =>
          epoch.current === generation && roomRef.current === room;
        const update = () => {
          if (!current()) return;
          const view = (p: Participant, local: boolean): VoiceMember => ({
            identity: p.identity,
            name: p.name || "Участник",
            local,
            speaking: p.isSpeaking,
            microphone: p.isMicrophoneEnabled,
            quality: String(p.connectionQuality),
          });
          setMembers([
            view(room.localParticipant, true),
            ...Array.from(room.remoteParticipants.values(), (p) =>
              view(p, false),
            ),
          ]);
          setMic(room.localParticipant.isMicrophoneEnabled);
        };
        room.on(RoomEvent.ConnectionStateChanged, (s) => {
          if (current()) setState(s);
        });
        for (const event of [
          RoomEvent.ParticipantConnected,
          RoomEvent.ParticipantDisconnected,
          RoomEvent.ActiveSpeakersChanged,
          RoomEvent.TrackMuted,
          RoomEvent.TrackUnmuted,
          RoomEvent.LocalTrackPublished,
          RoomEvent.LocalTrackUnpublished,
          RoomEvent.ConnectionQualityChanged,
          RoomEvent.ParticipantNameChanged,
        ])
          room.on(event, update);
        room.on(
          RoomEvent.TrackSubscribed,
          (track, _publication, participant) => {
            if (!current() || track.kind !== Track.Kind.Audio) return;
            const element = track.attach();
            element.dataset.voiceAudio = participant.identity;
            audioHostRef.current?.append(element);
            participant.setVolume(
              audioPrefs.current.deaf ? 0 : audioPrefs.current.volume,
            );
            update();
          },
        );
        room.on(RoomEvent.TrackUnsubscribed, (track) => {
          for (const element of track.detach()) element.remove();
          update();
        });
        room.on(RoomEvent.AudioPlaybackStatusChanged, () => {
          if (current()) setAudioBlocked(!room.canPlaybackAudio);
        });
        room.on(RoomEvent.MediaDevicesError, (e) => {
          if (current()) setError(voiceError(e));
        });
        room.on(RoomEvent.Disconnected, (reason) => {
          if (!current()) return;
          roomRef.current = null;
          room.removeAllListeners();
          audioHostRef.current?.replaceChildren();
          setPair(null);
          setMembers([]);
          setMic(false);
          setDeaf(false);
          setConnecting(false);
          setState(ConnectionState.Disconnected);
          setError(
            reason === DisconnectReason.DUPLICATE_IDENTITY
              ? "Вы подключились с другого устройства или вкладки. Здесь соединение завершено."
              : reason === DisconnectReason.CLIENT_INITIATED
                ? ""
                : "Соединение завершено. Войдите в комнату снова.",
          );
        });
        await room.connect(access.url, access.token);
        if (!current()) {
          await room.disconnect();
          return;
        }
        setPair(id);
        setState(room.state);
        setConnecting(false);
        update();
        await room.startAudio();
        if (current()) setAudioBlocked(!room.canPlaybackAudio);
      } catch (e) {
        if (epoch.current !== generation) return;
        next?.removeAllListeners();
        await next?.disconnect();
        roomRef.current = null;
        setPair(null);
        setMembers([]);
        setState(ConnectionState.Disconnected);
        setConnecting(false);
        if (!controller.signal.aborted) setError(voiceError(e));
      }
    },
    [input, output, leave],
  );

  const toggleMic = async () => {
    const room = roomRef.current;
    if (!room || busy || room.state !== ConnectionState.Connected) return;
    setBusy(true);
    setError("");
    try {
      await room.localParticipant.setMicrophoneEnabled(
        !room.localParticipant.isMicrophoneEnabled,
      );
    } catch (e) {
      if (roomRef.current === room) setError(voiceError(e));
    } finally {
      if (roomRef.current === room) setBusy(false);
    }
  };
  const toggleDeaf = async () => {
    const room = roomRef.current;
    if (!room || busy) return;
    setBusy(true);
    setError("");
    try {
      if (!deaf && room.localParticipant.isMicrophoneEnabled)
        await room.localParticipant.setMicrophoneEnabled(false);
      if (roomRef.current === room) setDeaf(!deaf);
    } catch (e) {
      if (roomRef.current === room) setError(voiceError(e));
    } finally {
      if (roomRef.current === room) setBusy(false);
    }
  };
  const switchDevice = async (
    kind: "audioinput" | "audiooutput",
    id: string,
  ) => {
    setError("");
    try {
      if (roomRef.current) await roomRef.current.switchActiveDevice(kind, id);
      if (kind === "audioinput") setInput(id);
      else setOutput(id);
    } catch (e) {
      setError(voiceError(e, "Не удалось выбрать устройство звука."));
    }
  };
  const startAudio = async () => {
    try {
      await roomRef.current?.startAudio();
      setAudioBlocked(false);
    } catch {
      setError("Браузер блокирует воспроизведение. Разрешите звук для сайта.");
    }
  };
  const setAudioHost = useCallback((node: HTMLDivElement | null) => {
    audioHostRef.current = node;
  }, []);
  return {
    pair,
    state,
    connecting,
    members,
    error,
    setError,
    mic,
    deaf,
    busy,
    volume,
    setVolume,
    input,
    output,
    switchDevice,
    audioBlocked,
    startAudio,
    setAudioHost,
    join,
    leave,
    toggleMic,
    toggleDeaf,
  };
}
