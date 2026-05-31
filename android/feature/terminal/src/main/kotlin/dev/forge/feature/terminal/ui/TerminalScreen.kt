package dev.forge.feature.terminal.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import dev.forge.feature.terminal.TerminalViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TerminalScreen(
    directory: String,
    onBack: () -> Unit,
    viewModel: TerminalViewModel = hiltViewModel(),
) {
    LaunchedEffect(directory) { viewModel.connect(directory) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        "Terminal",
                        fontFamily = FontFamily.Monospace,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = Color(0xFF1A1A1A),
                    titleContentColor = Color(0xFF00FF00),
                    navigationIconContentColor = Color.White,
                ),
            )
        },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .background(Color.Black),
        ) {
            // Scrolling terminal output
            val scrollState = rememberScrollState()

            LaunchedEffect(viewModel.lines.size) {
                scrollState.animateScrollTo(scrollState.maxValue)
            }

            Column(
                Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(scrollState)
                    .padding(8.dp),
            ) {
                viewModel.lines.forEach { line ->
                    Text(
                        text = line,
                        fontFamily = FontFamily.Monospace,
                        fontSize = 12.sp,
                        color = Color(0xFF00FF00),
                        lineHeight = 16.sp,
                    )
                }
            }

            // Input row
            var input by remember { mutableStateOf("") }
            HorizontalDivider(color = Color(0xFF333333), thickness = 1.dp)
            Row(
                Modifier
                    .fillMaxWidth()
                    .imePadding()
                    .background(Color(0xFF1A1A1A))
                    .padding(horizontal = 8.dp, vertical = 4.dp),
                verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
            ) {
                BasicTextField(
                    value = input,
                    onValueChange = { input = it },
                    modifier = Modifier
                        .weight(1f)
                        .background(Color(0xFF2A2A2A))
                        .padding(horizontal = 8.dp, vertical = 8.dp),
                    textStyle = TextStyle(
                        color = Color.White,
                        fontFamily = FontFamily.Monospace,
                        fontSize = 12.sp,
                    ),
                    singleLine = false,
                    cursorBrush = androidx.compose.ui.graphics.SolidColor(Color(0xFF00FF00)),
                )
                IconButton(
                    onClick = {
                        viewModel.sendInput(input + "\r")
                        input = ""
                    },
                ) {
                    Icon(
                        Icons.AutoMirrored.Filled.Send,
                        contentDescription = "Send",
                        tint = Color(0xFF00FF00),
                    )
                }
            }
        }
    }
}
