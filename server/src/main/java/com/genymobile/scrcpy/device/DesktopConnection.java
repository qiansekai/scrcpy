package com.genymobile.scrcpy.device;

import com.genymobile.scrcpy.control.ControlChannel;
import com.genymobile.scrcpy.util.StringUtils;

import java.io.Closeable;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.SocketTimeoutException;
import java.nio.charset.StandardCharsets;

public final class DesktopConnection implements Closeable {

    private static final int DEVICE_NAME_FIELD_LENGTH = 64;

    // Accept timeout so the resident server does not block forever when a
    // client does not connect a stream (e.g. --no-audio/--no-control) or
    // silently disappears. A SocketTimeoutException on the video accept is
    // propagated so the session ends and the daemon restarts the server.
    private static final int ACCEPT_TIMEOUT_MS = 5000;

    private final Socket videoSocket;
    private final Socket audioSocket;
    private final Socket controlSocket;
    private final ControlChannel controlChannel;

    private DesktopConnection(Socket videoSocket, Socket audioSocket, Socket controlSocket) throws IOException {
        this.videoSocket = videoSocket;
        this.audioSocket = audioSocket;
        this.controlSocket = controlSocket;
        if (controlSocket != null) {
            // device->PC control messages must not be delayed by Nagle
            controlSocket.setTcpNoDelay(true);
        }
        controlChannel = controlSocket != null ? new ControlChannel(controlSocket) : null;
    }

    private static Socket connect(String host, int port) throws IOException {
        return new Socket(host, port);
    }

    public static DesktopConnection open(int scid, boolean tunnelForward, int tunnelPort, boolean video, boolean audio, boolean control,
            boolean sendDummyByte) throws IOException {
        Socket videoSocket = null;
        Socket audioSocket = null;
        Socket controlSocket = null;
        try {
            if (tunnelForward) {
                try (ServerSocket serverSocket = new ServerSocket(tunnelPort, 0, InetAddress.getByName("0.0.0.0"))) {
                    serverSocket.setReuseAddress(true); // prevent EADDRINUSE on fast restart due to TIME_WAIT
                    serverSocket.setSoTimeout(ACCEPT_TIMEOUT_MS);
                    if (video) {
                        videoSocket = serverSocket.accept();
                        videoSocket.setKeepAlive(true); // prevent permanent block on silent network loss
                        if (sendDummyByte) {
                            // send one byte so the client may read() to detect a connection error
                            videoSocket.getOutputStream().write(0);
                            sendDummyByte = false;
                        }
                    }
                    if (audio) {
                        try {
                            audioSocket = serverSocket.accept();
                            audioSocket.setKeepAlive(true);
                            if (sendDummyByte) {
                                // send one byte so the client may read() to detect a connection error
                                audioSocket.getOutputStream().write(0);
                                sendDummyByte = false;
                            }
                        } catch (SocketTimeoutException e) {
                            audioSocket = null; // client did not connect audio, skip
                        }
                    }
                    if (control) {
                        try {
                            controlSocket = serverSocket.accept();
                            controlSocket.setKeepAlive(true);
                            if (sendDummyByte) {
                                // send one byte so the client may read() to detect a connection error
                                controlSocket.getOutputStream().write(0);
                                sendDummyByte = false;
                            }
                        } catch (SocketTimeoutException e) {
                            controlSocket = null; // client did not connect control, skip
                        }
                    }
                }
            } else {
                if (video) {
                    videoSocket = connect("127.0.0.1", tunnelPort);
                }
                if (audio) {
                    audioSocket = connect("127.0.0.1", tunnelPort);
                }
                if (control) {
                    controlSocket = connect("127.0.0.1", tunnelPort);
                }
            }
        } catch (IOException | RuntimeException e) {
            if (videoSocket != null) {
                videoSocket.close();
            }
            if (audioSocket != null) {
                audioSocket.close();
            }
            if (controlSocket != null) {
                controlSocket.close();
            }
            throw e;
        }

        return new DesktopConnection(videoSocket, audioSocket, controlSocket);
    }

    private Socket getFirstSocket() {
        if (videoSocket != null) {
            return videoSocket;
        }
        if (audioSocket != null) {
            return audioSocket;
        }
        return controlSocket;
    }

    public void shutdown() throws IOException {
        if (videoSocket != null) {
            videoSocket.shutdownInput();
            videoSocket.shutdownOutput();
        }
        if (audioSocket != null) {
            audioSocket.shutdownInput();
            audioSocket.shutdownOutput();
        }
        if (controlSocket != null) {
            controlSocket.shutdownInput();
            controlSocket.shutdownOutput();
        }
    }

    public void close() throws IOException {
        if (videoSocket != null) {
            videoSocket.close();
        }
        if (audioSocket != null) {
            audioSocket.close();
        }
        if (controlSocket != null) {
            controlSocket.close();
        }
    }

    public void sendDeviceMeta(String deviceName) throws IOException {
        byte[] buffer = new byte[DEVICE_NAME_FIELD_LENGTH];

        byte[] deviceNameBytes = deviceName.getBytes(StandardCharsets.UTF_8);
        int len = StringUtils.getUtf8TruncationIndex(deviceNameBytes, DEVICE_NAME_FIELD_LENGTH - 1);
        System.arraycopy(deviceNameBytes, 0, buffer, 0, len);
        // byte[] are always 0-initialized in java, no need to set '\0' explicitly

        OutputStream outputStream = getFirstSocket().getOutputStream();
        outputStream.write(buffer, 0, buffer.length);
    }

    public OutputStream getVideoOutputStream() throws IOException {
        return videoSocket.getOutputStream();
    }

    public OutputStream getAudioOutputStream() throws IOException {
        return audioSocket.getOutputStream();
    }

    public ControlChannel getControlChannel() {
        return controlChannel;
    }
}
