package com.genymobile.scrcpy.admin;

import com.genymobile.scrcpy.util.Ln;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;

/**
 * Root admin server for scrcpy-lan: stays root (unlike the mirroring Server which
 * drops to shell uid), listens on its own port and runs shell commands remotely.
 */
public final class AdminServer {

    private static final int DEFAULT_PORT = 27184;
    private static final int MAX_PAYLOAD = 256 * 1024;

    // Go -> device request types
    private static final int TYPE_SHELL = 0x10;
    // device -> Go response types
    private static final int TYPE_STREAM = 0x20;
    private static final int TYPE_RESULT = 0x21;

    private AdminServer() {
        // not instantiable
    }

    public static void main(String... args) throws Exception {
        int port = DEFAULT_PORT;
        for (int i = 0; i < args.length; i++) {
            if ("--admin-port".equals(args[i]) && i + 1 < args.length) {
                port = Integer.parseInt(args[i + 1]);
            }
        }

        Ln.initLogLevel(Ln.Level.INFO);
        Ln.i("Admin server (root) listening on :" + port);

        try (ServerSocket server = new ServerSocket(port)) {
            while (true) {
                Socket socket = server.accept();
                Ln.i("Admin client connected: " + socket.getRemoteSocketAddress());
                Thread thread = new Thread(() -> handle(socket), "admin-client");
                thread.start();
            }
        }
    }

    private static void handle(Socket socket) {
        try (socket) {
            DataInputStream in = new DataInputStream(socket.getInputStream());
            DataOutputStream out = new DataOutputStream(socket.getOutputStream());
            while (true) {
                int type = in.readUnsignedByte();
                int len = in.readInt();
                if (len < 0 || len > MAX_PAYLOAD) {
                    Ln.e("Invalid admin payload length: " + len);
                    return;
                }
                byte[] payload = new byte[len];
                in.readFully(payload);
                switch (type) {
                    case TYPE_SHELL:
                        runShell(out, new String(payload, StandardCharsets.UTF_8));
                        break;
                    default:
                        Ln.w("Unknown admin message type: " + type);
                        break;
                }
            }
        } catch (IOException e) {
            // client disconnected or socket error; drop the connection
        }
    }

    private static void runShell(DataOutputStream out, String cmd) throws IOException {
        Ln.i("shell: " + cmd);
        Process process;
        try {
            process = new ProcessBuilder("/system/bin/sh", "-c", cmd).start();
        } catch (IOException e) {
            writeResult(out, -1, "", e.getMessage());
            return;
        }
        Thread outThread = new Thread(() -> pump(process.getInputStream(), out), "shell-stdout");
        Thread errThread = new Thread(() -> pump(process.getErrorStream(), out), "shell-stderr");
        outThread.start();
        errThread.start();
        try {
            process.waitFor();
            outThread.join();
            errThread.join();
        } catch (InterruptedException e) {
            process.destroy();
            Thread.currentThread().interrupt();
            return;
        }
        writeResult(out, process.exitValue(), "", "");
    }

    private static void pump(InputStream in, DataOutputStream out) {
        try {
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) != -1) {
                synchronized (out) {
                    out.writeByte(TYPE_STREAM);
                    out.writeInt(n);
                    out.write(buf, 0, n);
                    out.flush();
                }
            }
        } catch (IOException e) {
            // socket closed; stop pumping
        }
    }

    private static void writeResult(DataOutputStream out, int exitCode, String stdout, String stderr) throws IOException {
        byte[] so = stdout.getBytes(StandardCharsets.UTF_8);
        byte[] se = stderr.getBytes(StandardCharsets.UTF_8);
        synchronized (out) {
            out.writeByte(TYPE_RESULT);
            out.writeInt(4 + 4 + so.length + 4 + se.length);
            out.writeInt(exitCode);
            out.writeInt(so.length);
            out.write(so);
            out.writeInt(se.length);
            out.write(se);
            out.flush();
        }
    }
}
