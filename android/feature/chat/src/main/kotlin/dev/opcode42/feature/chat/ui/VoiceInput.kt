package dev.opcode42.feature.chat.ui

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.util.Log
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import java.util.Locale

private const val VOICE_TAG = "VoiceInput"

// onRmsChanged reports loudness in dB over a loosely-defined range; these bounds
// map it to 0..1. Tuned for typical phone mics — quiet room ≈ floor, normal
// speech peaks near the ceiling.
private const val RMS_FLOOR = -2f
private const val RMS_CEIL = 10f

// Envelope shaping on the raw RMS: jump up quickly (attack), ease down (release)
// so the level "pops" on speech and settles smoothly — the UI springs over this.
private const val AMP_ATTACK = 0.6f
private const val AMP_RELEASE = 0.25f

// Small gap before relaunching a session in continuous mode; avoids
// ERROR_RECOGNIZER_BUSY from restarting too eagerly inside a callback.
private const val RESTART_DELAY_MS = 120L

// Errors that don't mean "give up" in continuous mode: NO_MATCH/SPEECH_TIMEOUT are
// the normal end of a quiet utterance; RECOGNIZER_BUSY is a transient restart race
// (the previous session hadn't fully torn down). All just trigger the next session
// after the regap rather than silently dropping the user out of dictation.
private val RECOVERABLE_ERRORS = setOf(
    SpeechRecognizer.ERROR_NO_MATCH,
    SpeechRecognizer.ERROR_SPEECH_TIMEOUT,
    SpeechRecognizer.ERROR_RECOGNIZER_BUSY,
)

/**
 * Drives a *continuous* [SpeechRecognizer] session for the composer: it keeps
 * listening across the natural utterance breaks that the platform recognizer
 * imposes (each `startListening` ends on a pause) by relaunching itself until
 * [stop]/[cancel]. Must only be touched from the main thread — all
 * `RecognitionListener` callbacks arrive there too, so the Compose-observable
 * [isListening]/[amplitude] reads are safe during composition.
 *
 * Partial transcripts stream out via `onPartial` as the user speaks; each
 * finalized utterance is delivered via `onFinal` (the caller commits it and the
 * next utterance appends — see [PromptInput]). [amplitude] is a 0..1 loudness
 * envelope for the mic animation.
 *
 * If a misbehaving provider never calls back after [start], [isListening] stays
 * true (the mic stays lit); a second tap routes to [stop] and recovers it.
 */
@Stable
class VoiceInputController internal constructor(
    private val context: Context,
    private val onPartial: (String) -> Unit,
    private val onFinal: (String) -> Unit,
) {
    /** Whether an on-device/cloud recognition provider exists. Cached at construction. */
    val isAvailable: Boolean = SpeechRecognizer.isRecognitionAvailable(context)

    // Spans the whole continuous session (stays true across internal restarts);
    // only stop()/cancel()/destroy()/a fatal error clear it.
    var isListening by mutableStateOf(false)
        private set

    /** 0..1 loudness envelope updated ~10x/sec while listening; 0 when idle. */
    var amplitude by mutableFloatStateOf(0f)
        private set

    private var recognizer: SpeechRecognizer? = null
    private val handler = Handler(Looper.getMainLooper())

    // SpeechRecognizer dispatches callbacks via a main-thread Handler, so a
    // partial/final result can already be queued when the user cancels. cancel()
    // and destroy() drop this flag so those late callbacks are ignored; stop()
    // leaves it set because we still want the final result it triggers.
    private var acceptResults = true

    private val restartRunnable = Runnable {
        if (isListening && acceptResults) beginSession()
    }

    private val listener = object : RecognitionListener {
        override fun onRmsChanged(rmsdB: Float) {
            amplitude = nextAmplitude(amplitude, normalizeRms(rmsdB))
        }

        override fun onPartialResults(partialResults: Bundle?) {
            if (!acceptResults) return
            partialResults?.firstText()?.let(onPartial)
        }

        override fun onResults(results: Bundle?) {
            if (acceptResults) results?.firstText()?.let(onFinal)
            amplitude = 0f
            // Keep going: a finalized utterance just ends one session, not the mic.
            if (isListening && acceptResults) scheduleRestart() else isListening = false
        }

        override fun onError(error: Int) {
            amplitude = 0f
            if (isListening && acceptResults && error in RECOVERABLE_ERRORS) {
                scheduleRestart() // normal end-of-utterance pause; restart silently
            } else {
                if (error !in RECOVERABLE_ERRORS) Log.w(VOICE_TAG, "recognition error: $error")
                isListening = false
            }
        }

        override fun onEndOfSpeech() {
            amplitude = 0f
        }

        override fun onReadyForSpeech(params: Bundle?) {}
        override fun onBeginningOfSpeech() {}
        override fun onBufferReceived(buffer: ByteArray?) {}
        override fun onEvent(eventType: Int, params: Bundle?) {}
    }

    /** Begin a continuous session. No-op if already listening or unavailable. */
    fun start() {
        if (isListening || !isAvailable) return
        acceptResults = true
        isListening = true
        beginSession()
    }

    private fun beginSession() {
        val r = recognizer ?: SpeechRecognizer.createSpeechRecognizer(context).also {
            it.setRecognitionListener(listener)
            recognizer = it
        }
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, Locale.getDefault().toLanguageTag())
            putExtra(RecognizerIntent.EXTRA_CALLING_PACKAGE, context.packageName)
        }
        r.startListening(intent)
    }

    private fun scheduleRestart() {
        handler.removeCallbacks(restartRunnable)
        handler.postDelayed(restartRunnable, RESTART_DELAY_MS)
    }

    /** End the session, delivering the in-progress utterance via `onFinal`. */
    fun stop() {
        if (!isListening) return
        isListening = false // set first so the resulting onResults won't restart
        handler.removeCallbacks(restartRunnable)
        amplitude = 0f
        recognizer?.stopListening()
    }

    /** Abort the session, discarding any pending result. Used by send and cancel. */
    fun cancel() {
        if (!isListening) return
        acceptResults = false
        isListening = false
        handler.removeCallbacks(restartRunnable)
        amplitude = 0f
        recognizer?.cancel()
    }

    /** Release the underlying recognizer. Call once when the composable leaves. */
    fun destroy() {
        acceptResults = false
        isListening = false
        handler.removeCallbacks(restartRunnable)
        amplitude = 0f
        recognizer?.destroy()
        recognizer = null
    }
}

/** Maps a raw `onRmsChanged` dB reading onto the 0..1 range used by the mic animation. */
internal fun normalizeRms(rmsdB: Float): Float =
    ((rmsdB - RMS_FLOOR) / (RMS_CEIL - RMS_FLOOR)).coerceIn(0f, 1f)

/** One asymmetric-EMA step from [prev] toward [norm]: jump up fast (attack), ease down (release). */
internal fun nextAmplitude(prev: Float, norm: Float): Float {
    val k = if (norm > prev) AMP_ATTACK else AMP_RELEASE
    return prev + (norm - prev) * k
}

private fun Bundle.firstText(): String? =
    getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
        ?.firstOrNull()
        ?.takeIf { it.isNotBlank() }

/**
 * Remembers a [VoiceInputController] tied to the current context, forwarding the
 * latest `onPartial`/`onFinal` lambdas without recreating the recognizer, and
 * destroying it on dispose.
 */
@Composable
fun rememberVoiceInput(
    onPartial: (String) -> Unit,
    onFinal: (String) -> Unit,
): VoiceInputController {
    val context = LocalContext.current
    val currentPartial by rememberUpdatedState(onPartial)
    val currentFinal by rememberUpdatedState(onFinal)
    val controller = remember(context) {
        VoiceInputController(
            context = context,
            onPartial = { currentPartial(it) },
            onFinal = { currentFinal(it) },
        )
    }
    DisposableEffect(controller) {
        onDispose { controller.destroy() }
    }
    return controller
}
