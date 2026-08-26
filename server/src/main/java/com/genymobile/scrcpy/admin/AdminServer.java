package com.genymobile.scrcpy.admin;

import com.genymobile.scrcpy.util.Ln;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.File;
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
    // 推送块 = 4B pathLen + path + data，data 单块 ≤256KB，加上 path 头放宽到 1MB。
    private static final int MAX_PAYLOAD = 1024 * 1024;

    // Go -> device request types
    private static final int TYPE_SHELL = 0x10;
    private static final int TYPE_PUSH = 0x11;
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
        // 每个连接一个推送目标文件：首块截断创建，后续追加；切换路径重新截断。
        String pushPath = null;
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
                    case TYPE_PUSH:
                        pushPath = handlePush(out, payload, pushPath);
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

    /**
     * PUSH 分块写文件：payload = int32 pathLen + path(UTF-8) + data。
     * 路径必须为绝对路径、不含 ".."，且前缀在允许列表内（防任意写）。
     * 每块写完后回一个 RESULT(exitCode=0) ack；连接关闭即文件完成。
     */
    private static String handlePush(DataOutputStream out, byte[] payload, String currentPath) throws IOException {
        if (payload.length < 4) {
            writeResult(out, -1, "", "short push payload");
            return currentPath;
        }
        int pathLen = ((payload[0] & 0xFF) << 24) | ((payload[1] & 0xFF) << 16)
                | ((payload[2] & 0xFF) << 8) | (payload[3] & 0xFF);
        if (pathLen < 1 || 4 + pathLen > payload.length) {
            writeResult(out, -1, "", "invalid path length");
            return currentPath;
        }
        String path = new String(payload, 4, pathLen, StandardCharsets.UTF_8);
        if (!isPushPathAllowed(path)) {
            writeResult(out, -1, "", "path not allowed: " + path);
            return currentPath;
        }
        try {
            File file = new File(path);
            File parent = file.getParentFile();
            if (parent != null && !parent.exists() && !parent.mkdirs()) {
                writeResult(out, -1, "", "cannot create parent dir: " + parent);
                return currentPath;
            }
            boolean append = path.equals(currentPath);
            try (java.io.FileOutputStream fos = new java.io.FileOutputStream(file, append)) {
                fos.write(payload, 4 + pathLen, payload.length - 4 - pathLen);
            }
            writeResult(out, 0, "", "");
            return path;
        } catch (IOException e) {
            writeResult(out, -1, "", e.getMessage());
            return currentPath;
        }
    }

    private static boolean isPushPathAllowed(String path) {
        if (!path.startsWith("/") || path.contains("..")) {
            return false;
        }
        return path.startsWith("/data/local/tmp/")
                || path.startsWith("/sdcard/")
                || path.startsWith("/storage/emulated/0/");
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
