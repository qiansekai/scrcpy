package com.genymobile.scrcpy.audio;

import android.media.MediaCodec;

import java.nio.ByteBuffer;

public interface AudioCapture {
    void checkCompatibility() throws AudioCaptureException;
    void start() throws AudioCaptureException;
    void stop();

    /**
     * Set whether the captured audio keeps playing on the device.
     * Only supported by {@link AudioPlaybackCapture}; a no-op otherwise.
     */
    default void setKeepPlayingOnDevice(boolean keepPlayingOnDevice) {
        // no-op by default
    }

    /**
     * Read a chunk of {@link AudioConfig#MAX_READ_SIZE} samples.
     *
     * @param outDirectBuffer The target buffer
     * @param outBufferInfo The info to provide to MediaCodec
     * @return the number of bytes actually read.
     */
    int read(ByteBuffer outDirectBuffer, MediaCodec.BufferInfo outBufferInfo);
}
