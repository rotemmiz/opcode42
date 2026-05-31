package dev.forge.feature.chat.ui

import android.util.Base64
import android.util.Log
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.AttachFile
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.forge.core.model.FilePartInput
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val TAG = "PromptInput"
private const val MAX_FILE_BYTES = 10 * 1024 * 1024  // 10 MB OOM guard

/** Bundles a file part with the human-readable name shown in the chip. */
private data class PendingAttachment(val part: FilePartInput, val name: String)

/**
 * Sticky bottom prompt input with file attachment support (C5).
 * Design: surfaceContainer bg, 2dp primary left border, 48dp min height,
 * paper-plane send button in primary color. (design/android/README.md §5)
 *
 * File I/O is dispatched to Dispatchers.IO via rememberCoroutineScope.
 * Files > 10 MB are silently skipped (logged at WARN) to prevent OOM.
 */
@Composable
fun PromptInput(
    onSend: (String, List<FilePartInput>) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var text by remember { mutableStateOf("") }
    var pendingAttachments by remember { mutableStateOf<List<PendingAttachment>>(emptyList()) }

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
            .imePadding()
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
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

        Row(verticalAlignment = Alignment.Bottom) {
            // Text field with 2dp primary left accent bar
            Box(
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(SurfaceContainer)
                    .border(1.dp, Hairline, RoundedCornerShape(6.dp)),
            ) {
                Row(Modifier.fillMaxWidth()) {
                    Box(
                        modifier = Modifier
                            .width(2.dp)
                            .heightIn(min = 48.dp)
                            .background(Primary),
                    )
                    BasicTextField(
                        value = text,
                        onValueChange = { text = it },
                        textStyle = TextStyle(
                            color = OnSurface,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 13.5.sp,
                        ),
                        cursorBrush = SolidColor(Primary),
                        modifier = Modifier
                            .weight(1f)
                            .padding(horizontal = 10.dp, vertical = 12.dp),
                        decorationBox = { inner ->
                            if (text.isEmpty()) {
                                Text(
                                    "Ask anything…  /  @",
                                    color = OnSurfaceGhost,
                                    fontFamily = FontFamily.Monospace,
                                    fontSize = 13.5.sp,
                                )
                            }
                            inner()
                        },
                    )
                }
            }

            Spacer(Modifier.width(6.dp))

            // Paperclip button — always shown, 48dp touch target
            IconButton(
                onClick = { filePicker.launch("*/*") },
                enabled = enabled,
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(6.dp)),
            ) {
                Icon(
                    Icons.Default.AttachFile,
                    contentDescription = "Attach file",
                    tint = if (enabled) OnSurfaceVariant else OnSurfaceFaint,
                    modifier = Modifier.size(20.dp),
                )
            }

            Spacer(Modifier.width(2.dp))

            // Send button — 48dp blue square (M3 minimum touch target)
            val canSend = enabled && (text.isNotBlank() || pendingAttachments.isNotEmpty())
            IconButton(
                onClick = {
                    val trimmed = text.trim()
                    onSend(trimmed, pendingAttachments.map { it.part })
                    text = ""
                    pendingAttachments = emptyList()
                },
                enabled = canSend,
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(if (canSend) Primary else Hairline),
            ) {
                Icon(
                    Icons.AutoMirrored.Filled.Send,
                    contentDescription = "Send",
                    tint = if (canSend) OnPrimary else OnSurfaceFaint,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
    }
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
