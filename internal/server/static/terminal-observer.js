(function () {
  const NativeWebSocket = window.WebSocket;
  let currentSocket = null;
  let outgoingFrames = 0;
  let outgoingInputFrames = 0;
  const publish = (state) => window.parent.postMessage({ type: "control-agents:terminal-transport", state }, window.location.origin);

  function ObservedWebSocket(url, protocols) {
    const socket = protocols === undefined ? new NativeWebSocket(url) : new NativeWebSocket(url, protocols);
    const nativeSend = socket.send;
    socket.send = function (data) {
      outgoingFrames += 1;
      let firstByte = null;
      if (typeof data === "string" && data.length > 0) {
        firstByte = data.charCodeAt(0);
      } else if (data instanceof ArrayBuffer && data.byteLength > 0) {
        firstByte = new Uint8Array(data, 0, 1)[0];
      } else if (ArrayBuffer.isView(data) && data.byteLength > 0) {
        firstByte = new Uint8Array(data.buffer, data.byteOffset, 1)[0];
      }
      if (firstByte === 48) outgoingInputFrames += 1;
      return nativeSend.call(socket, data);
    };
    currentSocket = socket;
    publish("CONNECTING");
    socket.addEventListener("open", () => {
      if (currentSocket === socket) publish("CONNECTED");
    });
    const disconnected = () => {
      if (currentSocket === socket) publish("CONNECTING");
    };
    socket.addEventListener("close", disconnected);
    socket.addEventListener("error", disconnected);
    return socket;
  }

  Object.setPrototypeOf(ObservedWebSocket, NativeWebSocket);
  ObservedWebSocket.prototype = NativeWebSocket.prototype;
  Object.defineProperty(window, "__controlAgentsTerminalSocket", {
    configurable: true,
    get: () => currentSocket
  });
  Object.defineProperty(window, "__controlAgentsTerminalOutgoingFrames", {
    configurable: true,
    get: () => outgoingFrames
  });
  Object.defineProperty(window, "__controlAgentsTerminalOutgoingInputFrames", {
    configurable: true,
    get: () => outgoingInputFrames
  });
  window.WebSocket = ObservedWebSocket;
})();
