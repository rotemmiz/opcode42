package dev.forge.feature.chat.ui

import android.util.Base64
import android.util.Log
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
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
import dev.forge.core.model.FilePartInput
import dev.forge.feature.chat.commands.PaletteEntry
import dev.forge.feature.chat.commands.filterByQuery
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val TAG = "PromptInput"
private const val MAX_FILE_BYTES = 10 * 1024 * 1024  // 10 MB OOM guard
private const val MAX_PALETTE_ROWS = 20               // max matches kept; the panel scrolls

/** Bundles a file part with the human-readable name shown in the chip. */
private data class PendingAttachment(val part: FilePartInput, val name: String)

// Trailing @-mention token at the caret end: "see @src/ht" → "src/ht".
private val MentionRegex = Regex("""(?:^|\s)@(\S*)$""")

/** A leading slash command split into the command [word] and its [arguments]. */
private data class SlashInput(val word: String, val arguments: String)

/**
 * Parses a leading "/command [args]" token. Returns null unless the text starts with
 * "/" and has no newline (a multi-line message isn't a command). "/dep" → word "dep",
 * empty args; "/deploy prod" → word "deploy", args "prod"; "/" → empty word (shows the
 * whole palette).
 */
private fun parseSlashInput(text: String): SlashInput? {
    if (!text.startsWith("/") || text.contains('\n')) return null
    val body = text.drop(1)
    val space = body.indexOf(' ')
    return if (space < 0) SlashInput(body, "")
    else SlashInput(body.take(space), body.substring(space + 1).trim())
}

/**
 * Sticky bottom prompt input (C5) with `/` command palette and `@` file-mention
 * autocomplete (design §5 + Interactions). Both surface as inline suggestion
 * panels above the field so the keyboard stays up while filtering.
 */
@Composable
fun PromptInput(
    onSend: (String, List<FilePartInput>) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    busy: Boolean = false,
    onStop: () -> Unit = {},
    paletteEntries: List<PaletteEntry> = emptyList(),
    onSearchFiles: suspend (String) -> List<String> = { emptyList() },
    onPickEntry: (PaletteEntry, arguments: String) -> Unit = { _, _ -> },
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var text by remember { mutableStateOf("") }
    var pendingAttachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }

    // A leading "/command [args]" token (no newline): the word filters the palette,
    // the rest is the command's arguments. Stays active while typing args so the
    // matching command can be tapped to run with them.
    val slash: SlashInput? = remember(text) { parseSlashInput(text) }

    val mentionMatch = remember(text) { MentionRegex.find(text) }
    val mentionQuery: String? = mentionMatch?.groupValues?.get(1)

    val filteredCommands = remember(paletteEntries, slash) {
        if (slash == null) emptyList()
        else paletteEntries.filterByQuery(slash.word).take(MAX_PALETTE_ROWS)
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

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 12.dp, top = 8.dp, bottom = 12.dp),
    ) {
        // ── Autocomplete panels (above the field) ──
        when {
            filteredCommands.isNotEmpty() -> CommandPanel(
                entries = filteredCommands,
                onPick = { entry ->
                    onPickEntry(entry, slash?.arguments.orEmpty())
                    text = ""
                },
            )
            mentionQuery != null && fileResults.isNotEmpty() -> MentionPanel(
                files = fileResults,
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

        // One bordered container holding the field, attach + send (design §5).
        // The 2dp primary rail is drawn relative to the measured height so it
        // always spans the box; children are centered vertically.
        val canSend = enabled && (text.isNotBlank() || pendingAttachments.isNotEmpty())
        val rail = Primary
        val shape = RoundedCornerShape(6.dp)
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 48.dp)
                .clip(shape)
                .background(SurfaceContainer)
                .border(1.dp, Hairline, shape)
                .drawBehind { drawRect(rail, size = Size(2.dp.toPx(), size.height)) }
                .padding(start = 13.dp),
        ) {
            BasicTextField(
                value = text,
                onValueChange = { text = it },
                textStyle = TextStyle(
                    color = OnSurface,
                    fontFamily = ForgeMono,
                    fontSize = 13.5.sp,
                ),
                cursorBrush = SolidColor(Primary),
                visualTransformation = composerTokenTransformation(Secondary, LinkCyan),
                modifier = Modifier
                    .weight(1f)
                    .padding(end = 4.dp, top = 13.dp, bottom = 13.dp),
                decorationBox = { inner ->
                    if (text.isEmpty()) {
                        Text(
                            "Ask anything…  /  @",
                            color = OnSurfaceGhost,
                            fontFamily = ForgeMono,
                            fontSize = 13.5.sp,
                        )
                    }
                    inner()
                },
            )

            // Attach (add) icon — vertically centered by the Row
            IconButton(
                onClick = { filePicker.launch("*/*") },
                enabled = enabled,
                modifier = Modifier.size(44.dp),
            ) {
                Icon(
                    Icons.Default.AttachFile,
                    contentDescription = "Attach file",
                    tint = if (enabled) OnSurfaceVariant else OnSurfaceFaint,
                    modifier = Modifier.size(19.dp),
                )
            }

            // Trailing action — 40dp square in a 48dp touch target, centered by the Row.
            // While the agent is running it becomes a Stop button; otherwise it's Send.
            val active = busy || canSend
            Box(
                modifier = Modifier
                    .padding(end = 4.dp)
                    .size(48.dp)
                    .clickable(enabled = active) {
                        if (busy) {
                            onStop()
                            return@clickable
                        }
                        // A fully-typed "/command [args]" runs the command instead of
                        // being sent as a chat message (so `/deploy prod` + Send works).
                        val match = slash?.let { s ->
                            paletteEntries.firstOrNull { it.enabled && it.name.equals(s.word, ignoreCase = true) }
                        }
                        if (match != null) {
                            onPickEntry(match, slash.arguments)
                            text = ""
                        } else {
                            onSend(text.trim(), pendingAttachments.map { it.part })
                            text = ""
                            pendingAttachments = emptyList()
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                Box(
                    modifier = Modifier
                        .size(40.dp)
                        .clip(shape)
                        .background(
                            when {
                                busy -> Error
                                canSend -> Primary
                                else -> Hairline
                            },
                        ),
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
}

/**
 * Slash-command suggestions list, anchored above the field. Merges built-in client
 * actions (first) with daemon commands; disabled entries (not-yet-built built-ins)
 * render greyed with a "soon" badge and are not selectable.
 */
@Composable
private fun CommandPanel(entries: List<PaletteEntry>, onPick: (PaletteEntry) -> Unit) {
    SuggestionPanel {
        LazyColumn {
            itemsIndexed(entries, key = { _, e -> e.key }) { index, entry ->
                if (index > 0) HorizontalDivider(color = Hairline)
                val rowModifier = Modifier
                    .fillMaxWidth()
                    .then(if (entry.enabled) Modifier.clickable { onPick(entry) } else Modifier)
                    .heightIn(min = 48.dp)
                    .padding(horizontal = 14.dp, vertical = 8.dp)
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = rowModifier,
                ) {
                    Text(
                        text = "/${entry.name}",
                        fontFamily = ForgeMono,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.Medium,
                        color = if (entry.enabled) Secondary else OnSurfaceFaint,
                    )
                    entry.description?.let {
                        Text(
                            text = it,
                            fontSize = 13.sp,
                            color = if (entry.enabled) OnSurfaceVariant else OnSurfaceFaint,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                    }
                    entry.badge?.let { SourcePill(it) }
                }
            }
        }
    }
}

/** @-mention file suggestions list, anchored above the field. */
@Composable
private fun MentionPanel(files: List<String>, onPick: (String) -> Unit) {
    SuggestionPanel {
        LazyColumn {
            items(files, key = { it }) { path ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { onPick(path) }
                        .heightIn(min = 44.dp)
                        .padding(horizontal = 14.dp, vertical = 6.dp),
                ) {
                    Text(
                        text = path,
                        fontFamily = ForgeMono,
                        fontSize = 13.sp,
                        color = LinkCyan,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

@Composable
private fun SuggestionPanel(content: @Composable () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(max = 320.dp)
            .padding(bottom = 6.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(SurfaceContainerHigh)
            .border(1.dp, Hairline, RoundedCornerShape(8.dp)),
    ) {
        content()
    }
}

@Composable
private fun SourcePill(source: String) {
    Text(
        text = source,
        fontFamily = ForgeMono,
        fontSize = 11.sp,
        color = LinkCyan,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(LinkCyan.copy(alpha = 0.12f))
            .padding(horizontal = 6.dp, vertical = 1.dp),
    )
}

/** Colors a leading `/command` [accent] and any `@mention` [mention] (length-preserving). */
private fun composerTokenTransformation(accent: Color, mention: Color) = VisualTransformation { original ->
    val str = original.text
    val annotated = buildAnnotatedString {
        append(str)
        if (str.startsWith("/")) {
            val end = str.indexOf(' ').let { if (it == -1) str.length else it }
            addStyle(SpanStyle(color = accent), 0, end)
        }
        Regex("""@\S+""").findAll(str).forEach { m ->
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
