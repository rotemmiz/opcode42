package dev.opcode42.feature.chat.ui

import dev.opcode42.core.design.theme.*

import android.Manifest
import android.content.pm.PackageManager
import android.util.Base64
import android.util.Log
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.filled.AlternateEmail
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.OffsetMapping
import androidx.compose.ui.text.input.TransformedText
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import dev.opcode42.core.model.FilePartInput
import dev.opcode42.feature.chat.commands.PaletteEntry
import dev.opcode42.feature.chat.commands.filterByQuery
import dev.opcode42.feature.chat.commands.parseSlashCommand
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val TAG = "PromptInput"
private const val MAX_FILE_BYTES = 10 * 1024 * 1024  // 10 MB OOM guard
private const val MAX_PALETTE_ROWS = 20               // panel scrolls past this

/** Bundles a file part with the human-readable name shown in the chip. */
private data class PendingAttachment(val part: FilePartInput, val name: String)

// Trailing @-mention token at the caret end: "see @src/ht" → "src/ht".
private val MentionRegex = Regex("""(?:^|\s)@(\S*)$""")

/** Appends dictated [spoken] text to whatever was in the field ([base]) when dictation began. */
internal fun mergeTranscript(base: String, spoken: String): String =
    if (base.isBlank()) spoken else "${base.trimEnd()} $spoken"

/**
 * Sticky bottom prompt input (C5) with `/` command palette and `@` file-mention
 * autocomplete (design §5 + Interactions). Both surface as inline suggestion
 * panels above the field so the keyboard stays up while filtering.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PromptInput(
    onSend: (String, List<FilePartInput>) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    busy: Boolean = false,
    onStop: () -> Unit = {},
    paletteEntries: List<PaletteEntry> = emptyList(),
    onSearchFiles: suspend (String) -> List<String> = { emptyList() },
    onPickEntry: (PaletteEntry, String) -> Unit = { _, _ -> },
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var text by remember { mutableStateOf("") }
    var pendingAttachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }
    // `baseText` advances as each utterance is finalized: live partials append to
    // it, then the final commits and becomes the new anchor for the next utterance.
    // `preDictationText` is the field as it was before the mic was tapped — what
    // Cancel restores.
    var baseText by remember { mutableStateOf("") }
    var preDictationText by remember { mutableStateOf("") }

    // `/cmd` (and `/cmd args…`) is active while the text starts with a slash and
    // has no newline. The command name (before the first space) drives palette
    // filtering; the remainder becomes the args string passed to runCommand.
    val slashInput = remember(text) { parseSlashCommand(text) }
    val slashQuery: String? = slashInput?.name
    val slashArgs: String = slashInput?.args ?: ""
    val mentionMatch = remember(text) { MentionRegex.find(text) }
    val mentionQuery: String? = mentionMatch?.groupValues?.get(1)

    val filteredCommands = remember(paletteEntries, slashQuery) {
        if (slashQuery == null) emptyList()
        else paletteEntries.filterByQuery(slashQuery).take(MAX_PALETTE_ROWS)
    }

    var fileResults by remember { mutableStateOf<List<String>>(emptyList()) }
    // Keep the latest search lambda without restarting the effect when only it changes.
    val currentSearch by rememberUpdatedState(onSearchFiles)
    LaunchedEffect(mentionQuery) {
        fileResults = if (mentionQuery != null && mentionQuery.length >= 1) {
            currentSearch(mentionQuery)
        } else {
            emptyList()
        }
    }

    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch(Dispatchers.IO) {
            val mime = context.contentResolver.getType(uri) ?: "application/octet-stream"
            val bytes = context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
                ?: return@launch
            if (bytes.size > MAX_FILE_BYTES) {
                Log.w(TAG, "Skipping attachment: ${bytes.size} bytes exceeds 10 MB limit")
                return@launch
            }
            val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
            val dataUrl = "data:$mime;base64,$b64"
            val name = context.contentResolver.query(
                uri,
                arrayOf(android.provider.OpenableColumns.DISPLAY_NAME),
                null, null, null,
            )?.use { cursor ->
                if (cursor.moveToFirst()) cursor.getString(0) else null
            } ?: uri.lastPathSegment ?: "file"

            withContext(Dispatchers.Main) {
                pendingAttachments = pendingAttachments + PendingAttachment(
                    FilePartInput(mime = mime, url = dataUrl), name,
                )
            }
        }
    }

    // Continuous speech-to-text. Live partials preview against the current anchor;
    // each final commits — advancing `baseText` — so the next utterance appends
    // after it with a space instead of overwriting.
    val voice = rememberVoiceInput(
        onPartial = { spoken -> text = mergeTranscript(baseText, spoken) },
        onFinal = { spoken ->
            baseText = mergeTranscript(baseText, spoken)
            text = baseText
        },
    )
    // Anchor the field and begin dictation. Both the already-granted path and the
    // permission callback funnel through here so the anchors are captured exactly
    // once, immediately before the recognizer starts.
    fun startDictation() {
        preDictationText = text
        baseText = text
        voice.start()
    }
    // Stop listening and throw away everything dictated this session. Guarded so a
    // tap on the ✕ during its exit animation (after listening already ended) can't
    // wipe a just-committed transcript.
    fun cancelDictation() {
        if (!voice.isListening) return
        voice.cancel()
        text = preDictationText
        baseText = preDictationText
    }
    val audioPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted) {
            startDictation()
        } else {
            // Denial (incl. permanent "don't ask again", which returns instantly
            // with no dialog) would otherwise be a silent dead end.
            Toast.makeText(
                context, "Microphone access is needed for voice input", Toast.LENGTH_SHORT,
            ).show()
        }
    }
    fun toggleVoice() {
        if (voice.isListening) {
            voice.stop()
            return
        }
        val granted = ContextCompat.checkSelfPermission(
            context, Manifest.permission.RECORD_AUDIO,
        ) == PackageManager.PERMISSION_GRANTED
        if (granted) startDictation() else audioPermission.launch(Manifest.permission.RECORD_AUDIO)
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 12.dp, top = 8.dp, bottom = 12.dp),
    ) {
        // ── Autocomplete panels (above the field) ──
        when {
            filteredCommands.isNotEmpty() -> CommandPanel(
                entries = filteredCommands,
                query = slashQuery ?: "",
                onPick = { entry ->
                    onPickEntry(entry, slashArgs)
                    text = ""
                },
            )
            mentionQuery != null && fileResults.isNotEmpty() -> MentionPanel(
                files = fileResults,
                query = mentionQuery,
                onPick = { path ->
                    val start = mentionMatch!!.range.first + (mentionMatch.value.length - mentionMatch.value.trimStart().length)
                    text = text.substring(0, start) + "@" + path + " "
                },
            )
        }

        // Attachment chip strip — shown only when there are attachments
        if (pendingAttachments.isNotEmpty()) {
            LazyRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                contentPadding = PaddingValues(bottom = 6.dp),
            ) {
                itemsIndexed(
                    pendingAttachments,
                    key = { _, att -> att.part.url },
                ) { idx, att ->
                    AttachmentChip(
                        name = att.name,
                        onRemove = {
                            pendingAttachments = pendingAttachments
                                .toMutableList()
                                .also { it.removeAt(idx) }
                        },
                    )
                }
            }
        }

        // ── Composer container ──
        // A single pill-shaped surface (CircleShape when single-line, morphs to a
        // rounded rectangle as text wraps to multiple lines). All icons live INSIDE
        // the container: + on the far left, mic + send on the far right. The text
        // field sits between them, vertically centered when single-line and
        // top-aligned when expanded.
        val canSend = enabled && (text.isNotBlank() || pendingAttachments.isNotEmpty())
        val selectionColors = androidx.compose.foundation.text.selection.LocalTextSelectionColors.current
        val accent = Secondary
        val mention = LinkCyan
        val composerTransform = remember(accent, mention) { composerTokenTransformation(accent, mention) }
        val interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() }
        val isFocused by interactionSource.collectIsFocusedAsState()
        // Pill (CircleShape) when the field is short; rounded rectangle once it wraps.
        val isMultiline = remember(text) { text.contains('\n') }
        val containerShape = if (isMultiline) RoundedCornerShape(24.dp) else CircleShape
        val borderColor = if (isFocused) Outline else Hairline

        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .clip(containerShape)
                .border(1.dp, borderColor, containerShape)
                .background(SurfaceContainer),
        ) {
            // Leading: + (attach) — default 48dp IconButton, vertically centered.
            IconButton(
                onClick = { filePicker.launch("*/*") },
                enabled = enabled,
                modifier = Modifier.size(48.dp),
            ) {
                Icon(
                    Icons.Default.AttachFile,
                    contentDescription = "Attach file",
                    tint = if (enabled) OnSurfaceVariant else OnSurfaceFaint,
                    modifier = Modifier.size(22.dp),
                )
            }

            // Text field — fills the remaining width; top-aligned so wrapped lines
            // grow upward while the icon row stays anchored at the bottom.
            BasicTextField(
                value = text,
                onValueChange = { text = it },
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp, max = 200.dp)
                    .padding(vertical = 14.dp),
                enabled = enabled,
                textStyle = Opcode42Typography.bodyLarge.copy(color = OnSurface),
                cursorBrush = SolidColor(selectionColors.handleColor),
                keyboardOptions = KeyboardOptions(autoCorrectEnabled = true),
                visualTransformation = composerTransform,
                maxLines = 8,
                interactionSource = interactionSource,
            ) { innerTextField ->
                if (text.isEmpty()) {
                    Text(
                        "Ask anything…  /  @",
                        color = OnSurfaceGhost,
                        fontSize = 16.sp,
                        maxLines = 1,
                        softWrap = false,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                innerTextField()
            }

            // Trailing controls — mic (if available) + send, anchored to the bottom-right.
            // While dictating: X (cancel/discard) appears to the LEFT of the mic, which
            // stays in place as the stop-and-keep button (red halo pulsing with amplitude).
            // Tapping the mic again stops dictation and keeps the text; tapping X discards.
            if (voice.isAvailable) {
                AnimatedVisibility(visible = voice.isListening) {
                    IconButton(
                        onClick = { cancelDictation() },
                        modifier = Modifier.size(48.dp),
                    ) {
                        Icon(
                            Icons.Default.Close,
                            contentDescription = "Cancel dictation",
                            tint = OnSurfaceVariant,
                            modifier = Modifier.size(22.dp),
                        )
                    }
                }
                MicButton(voice = voice, enabled = enabled, onToggle = { toggleVoice() })
            }

            // Send — solid circular button, anchored to the far right inside the container.
            val active = busy || canSend
            Box(
                modifier = Modifier
                    .padding(end = 6.dp, top = 6.dp, bottom = 6.dp)
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(
                        when {
                            busy -> Error
                            canSend -> Primary
                            else -> Hairline
                        },
                    )
                    .clickable(enabled = active) {
                        if (busy) {
                            onStop()
                        } else {
                            voice.cancel()
                            val trimmed = text.trim()
                            onSend(trimmed, pendingAttachments.map { it.part })
                            text = ""
                            pendingAttachments = emptyList()
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (busy) Icons.Default.Stop else Icons.AutoMirrored.Filled.Send,
                    contentDescription = if (busy) "Stop" else "Send",
                    tint = if (active) OnPrimary else OnSurfaceFaint,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
    }
}

/**
 * Mic button. Tap to start; while listening it is the stop-and-keep button and
 * draws an amplitude halo via [drawBehind] at the draw area's center, so the
 * circle stays concentric with the icon regardless of layout. `pulse` springs
 * over the controller's ~10Hz amplitude envelope; reading it inside drawBehind
 * keeps the animation in the draw phase (no per-frame recomposition), and reading
 * `voice` here confines the 10Hz invalidations to this node.
 */
@Composable
private fun MicButton(
    voice: VoiceInputController,
    enabled: Boolean,
    onToggle: () -> Unit,
) {
    val pulse by animateFloatAsState(
        targetValue = if (voice.isListening) voice.amplitude else 0f,
        // Envelope shaping lives in the controller (fast attack / slow release);
        // the spring just glides between frames. Lightly under-damped for a touch
        // of life without visible wobble.
        animationSpec = spring(dampingRatio = 0.8f, stiffness = Spring.StiffnessMediumLow),
        label = "micPulse",
    )
    // Read the themed color in composition; drawBehind runs in the draw phase where
    // the @Composable color getter isn't callable.
    val haloColor = Error
    IconButton(
        onClick = onToggle,
        enabled = enabled || voice.isListening,
        modifier = Modifier
            .size(44.dp)
            .drawBehind {
                if (!voice.isListening) return@drawBehind
                // Base radius 10dp, growing with amplitude; capped just inside the
                // 44dp button so it never clips the edge.
                val radius = (10.dp.toPx() * (1f + pulse * 1.1f)).coerceAtMost(21.dp.toPx())
                drawCircle(
                    color = haloColor,
                    radius = radius,
                    center = center,
                    alpha = 0.18f + pulse * 0.22f,
                )
            },
    ) {
        Icon(
            Icons.Default.Mic,
            contentDescription = if (voice.isListening) "Stop dictation" else "Dictate",
            tint = when {
                voice.isListening -> Error
                enabled -> OnSurfaceVariant
                else -> OnSurfaceFaint
            },
            modifier = Modifier.size(19.dp),
        )
    }
}

/**
 * Slash-command suggestions list, anchored above the field. Merges built-in client
 * actions (first) with daemon commands; disabled entries (not-yet-built built-ins)
 * render greyed with a "soon" badge and are not selectable. The top match is the
 * amber focal row; commands that open a further picker show a trailing chevron.
 */
@Composable
private fun CommandPanel(entries: List<PaletteEntry>, query: String, onPick: (PaletteEntry) -> Unit) {
    SuggestionPanel(
        header = {
            PanelHeader(
                leadingIcon = Icons.Default.Search,
                query = "/$query",
                trailingLabel = pluralize(entries.size, "command"),
            )
        },
    ) {
        LazyColumn {
            itemsIndexed(entries, key = { _, e -> e.key }) { index, entry ->
                val focal = index == 0 && entry.enabled
                val rowModifier = Modifier
                    .fillMaxWidth()
                    .then(if (entry.enabled) Modifier.clickable { onPick(entry) } else Modifier)
                    .focalRow(active = focal)
                    .heightIn(min = 48.dp)
                    .padding(horizontal = 14.dp, vertical = 8.dp)
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    modifier = rowModifier,
                ) {
                    Text(
                        text = "/${entry.name}",
                        fontFamily = Opcode42Mono,
                        fontSize = 13.sp,
                        fontWeight = if (focal) FontWeight.Bold else FontWeight.Medium,
                        color = when {
                            !entry.enabled -> OnSurfaceFaint
                            focal -> OnSurface
                            else -> LinkCyan
                        },
                    )
                    val desc = entry.description
                    if (desc != null) {
                        Text(
                            text = desc,
                            fontSize = 13.sp,
                            color = if (entry.enabled) OnSurfaceVariant else OnSurfaceFaint,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                    } else {
                        Spacer(Modifier.weight(1f))
                    }
                    val badge = entry.badge
                    when {
                        entry.hasSubmenu -> Icon(
                            Icons.AutoMirrored.Filled.KeyboardArrowRight,
                            contentDescription = null,
                            tint = OnSurfaceFaint,
                            modifier = Modifier.size(18.dp),
                        )
                        badge != null -> SourcePill(badge)
                    }
                }
            }
        }
    }
}

/**
 * @-mention file suggestions, anchored above the field. Each row shows a file glyph,
 * the filename (green), and its parent directory (dim); the top match is the amber
 * focal row.
 */
@Composable
private fun MentionPanel(files: List<String>, query: String, onPick: (String) -> Unit) {
    SuggestionPanel(
        header = {
            PanelHeader(
                leadingIcon = Icons.Default.AlternateEmail,
                query = "@$query",
                trailingLabel = "files",
            )
        },
    ) {
        LazyColumn {
            itemsIndexed(files, key = { _, path -> path }) { index, path ->
                val name = path.substringAfterLast('/')
                val parent = path.substringBeforeLast('/', "")
                val dir = if (parent.isEmpty()) "./" else "$parent/"
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { onPick(path) }
                        .focalRow(active = index == 0)
                        .heightIn(min = 44.dp)
                        .padding(horizontal = 14.dp, vertical = 6.dp),
                ) {
                    Icon(
                        Icons.AutoMirrored.Outlined.InsertDriveFile,
                        contentDescription = null,
                        tint = OnSurfaceFaint,
                        modifier = Modifier.size(15.dp),
                    )
                    Text(
                        text = name,
                        fontFamily = Opcode42Mono,
                        fontSize = 13.sp,
                        fontWeight = if (index == 0) FontWeight.Bold else FontWeight.Normal,
                        color = if (index == 0) OnSurface else Tertiary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = dir,
                        fontFamily = Opcode42Mono,
                        fontSize = 12.sp,
                        color = OnSurfaceFaint,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

/**
 * The floating suggestion surface above the composer: an elevated, rounded panel
 * with a header ([header]) over a divider, then the scrolling [content].
 */
@Composable
private fun SuggestionPanel(
    header: @Composable () -> Unit,
    content: @Composable () -> Unit,
) {
    val shape = RoundedCornerShape(16.dp)
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp)
            .shadow(16.dp, shape, clip = false)
            .clip(shape)
            .background(SurfaceContainerHigh)
            .border(1.dp, Hairline, shape),
    ) {
        header()
        HorizontalDivider(color = Hairline)
        Box(modifier = Modifier.heightIn(max = 280.dp)) {
            content()
        }
    }
}

/** Header row shared by the slash/@ panels: leading glyph, the live query, a kicker. */
@Composable
private fun PanelHeader(leadingIcon: ImageVector, query: String, trailingLabel: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 44.dp)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        Icon(
            leadingIcon,
            contentDescription = null,
            tint = OnSurfaceFaint,
            modifier = Modifier.size(16.dp),
        )
        Spacer(Modifier.width(10.dp))
        Text(
            text = query,
            fontFamily = Opcode42Mono,
            fontSize = 13.5.sp,
            color = OnSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = trailingLabel,
            fontFamily = Opcode42Mono,
            fontSize = 12.sp,
            color = OnSurfaceFaint,
        )
    }
}

/** "1 command" / "8 commands". */
private fun pluralize(count: Int, noun: String): String =
    if (count == 1) "1 $noun" else "$count ${noun}s"

@Composable
private fun SourcePill(source: String) {
    Text(
        text = source,
        fontFamily = Opcode42Mono,
        fontSize = 11.sp,
        color = LinkCyan,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(LinkCyan.copy(alpha = 0.12f))
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

private val MentionToken = Regex("""@\S+""")

/** Colors a leading `/command` [accent] and any `@mention` [mention] (length-preserving). */
private fun composerTokenTransformation(accent: Color, mention: Color) = VisualTransformation { original ->
    val str = original.text
    val annotated = buildAnnotatedString {
        append(str)
        if (str.startsWith("/")) {
            val end = str.indexOf(' ').let { if (it == -1) str.length else it }
            addStyle(SpanStyle(color = accent), 0, end)
        }
        MentionToken.findAll(str).forEach { m ->
            addStyle(SpanStyle(color = mention), m.range.first, m.range.last + 1)
        }
    }
    TransformedText(annotated, OffsetMapping.Identity)
}

/** Chip showing the attached file name with an X remove button. */
@Composable
private fun AttachmentChip(
    name: String,
    onRemove: () -> Unit,
) {
    Surface(
        shape = RoundedCornerShape(16.dp),
        color = SurfaceContainer,
        border = BorderStroke(1.dp, Hairline),
        tonalElevation = 0.dp,
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(start = 10.dp, end = 2.dp, top = 4.dp, bottom = 4.dp),
        ) {
            Text(
                text = name,
                style = MaterialTheme.typography.labelSmall,
                color = OnSurface,
                maxLines = 1,
            )
            // 48dp touch target for accessibility compliance
            IconButton(
                onClick = onRemove,
                modifier = Modifier.size(48.dp),
            ) {
                Icon(
                    Icons.Default.Close,
                    contentDescription = "Remove $name",
                    modifier = Modifier.size(14.dp),
                    tint = OnSurfaceFaint,
                )
            }
        }
    }
}
