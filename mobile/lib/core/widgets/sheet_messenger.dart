import 'package:flutter/material.dart';

/// Gives a bottom sheet its own [ScaffoldMessenger].
///
/// A SnackBar raised from inside a modal sheet goes to the page's messenger
/// and renders behind the sheet, where nobody sees it (see `InlineError` for
/// the long version). A sheet whose failures are worth a SnackBar -- the
/// server's own sentence, say, when it refuses a change -- wraps its content
/// in this, and `ScaffoldMessenger.of(context)` inside it shows above the
/// sheet.
///
/// The sheet takes [heightFactor] of the screen: a Scaffold fills whatever
/// it is given, so it needs a definite height to sit in.
class SheetMessenger extends StatelessWidget {
  const SheetMessenger({
    super.key,
    required this.child,
    this.heightFactor = 0.86,
  });

  final Widget child;
  final double heightFactor;

  @override
  Widget build(BuildContext context) {
    final height = MediaQuery.sizeOf(context).height * heightFactor;
    return SizedBox(
      height: height,
      child: ScaffoldMessenger(
        child: Scaffold(
          backgroundColor: Colors.transparent,
          resizeToAvoidBottomInset: false,
          body: child,
        ),
      ),
    );
  }
}
